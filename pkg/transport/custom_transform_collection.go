/*
Copyright 2024 The KubeStellar Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package transport

import (
	"context"
	"fmt"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	"github.com/kubestellar/kubestellar/api/control/v1alpha1"
	controlclient "github.com/kubestellar/kubestellar/pkg/generated/clientset/versioned/typed/control/v1alpha1"
	"github.com/kubestellar/kubestellar/pkg/jsonpath"
)

// customTransformCollection digests CustomTransform objects and caches the results.
type customTransformCollection struct {
	// client is here for updating the status of a CustomTransform
	client controlclient.CustomTransformInterface

	// getTransformObjects is the part of the CustomTransform informer's cache.Indexer behavior
	// that is needed here, for using the index named in `customTransformDomainIndexName`.
	// It is used to get the CustomTransform objects relevant to a given GroupResource.
	getTransformObjects func(indexName, indexedValue string) ([]any, error)

	// enqueue is used to add a reference to a Binding that needs to be re-processed because
	// of a change to a CustomTransform that the Binding is sensitive to.
	enqueue func(any)

	// mutex must be locked while accessing the following fields or their contents.
	// The comments on the following fields and on groupResourceTransformData
	// are things that hold true while the mutex is not locked.
	mutex sync.Mutex

	// grToTransformData has an entry for every GroupResource that some Binding cares about
	// (i.e., lists an object of that GroupResource), and no more entries.
	grToTransformData map[metav1.GroupResource]*groupResourceTransformData

	// ctNameToSpec holds, for each CustomTranform whose spec contributed to an
	// entry in grToTransformData, that CustomTransformSpec.
	ctNameToSpec map[string]v1alpha1.CustomTransformSpec

	// bindingNameToGroupResources tracks the set of GroupResource that each Binding
	// references. This is so that when the set for a given Binding changes,
	// for the GroupResources that are no longer in the set, the Binding's Name can
	// be removed from groupResourceTransformData.bindingsThatCare.
	// No Set[GroupResource] here is empty.
	bindingNameToGroupResources map[string]sets.Set[metav1.GroupResource]
}

// groupResourceTransformData is the ingested custom transforms for a given GroupResource
type groupResourceTransformData struct {
	bindingsThatCare sets.Set[string /*Binding name*/] // not empty
	ctNames          sets.Set[string /* CustomTransform name*/]
	changes          customTransformChanges
}

type customTransformChanges struct {
	removes []jsonpath.Query // immutable
}

func newCustomTransformCollection(client controlclient.CustomTransformInterface, getTransformObjects func(indexName, indexedValue string) ([]any, error), enqueue func(any)) *customTransformCollection {
	return &customTransformCollection{
		client:                      client,
		getTransformObjects:         getTransformObjects,
		enqueue:                     enqueue,
		grToTransformData:           make(map[metav1.GroupResource]*groupResourceTransformData),
		ctNameToSpec:                make(map[string]v1alpha1.CustomTransformSpec),
		bindingNameToGroupResources: make(map[string]sets.Set[metav1.GroupResource]),
	}
}

// GetCustomTransformData returns the groupResourceTransformData to use
// for the given GroupResource on behalf of the named Binding.
// This method returns a cached answer if one is available, otherwise
// digests the relevant CustomTransform object(s) and caches the result.
// Always records the fact that the given binding depends on the answer.
func (ctc *customTransformCollection) GetCustomTransformChanges(ctx context.Context, groupResource metav1.GroupResource, bindingName string) customTransformChanges {
	logger := klog.FromContext(ctx)
	ctc.mutex.Lock()
	defer ctc.mutex.Unlock()
	grTransformData, ok := ctc.grToTransformData[groupResource]
	if ok {
		grTransformData.bindingsThatCare.Insert(bindingName)
		return grTransformData.changes
	}
	grTransformData = &groupResourceTransformData{
		bindingsThatCare: sets.New(bindingName),
		ctNames:          sets.New[string](),
	}
	ctKey := customTransformDomainKey(groupResource.Group, groupResource.Resource)
	ctAnys, err := ctc.getTransformObjects(customTransformDomainIndexName, ctKey)
	if err != nil {
		// This only happens if the index is not defined;
		// that is, it never happens.
		// If it does, retry will not help.
		logger.Error(err, "Failed to get objects from CustomTransform domain index", "key", ctKey)
	}

	// Digest each relevant CustomTransform, accumulating remove instructions in groupResourceTransformData.removes.
	// Invalidate cache entry for each CustomTransform that changed its Spec's .Group or .Resource.
	for _, ctAny := range ctAnys {
		ct := ctAny.(*v1alpha1.CustomTransform)
		removes := ctc.digestCustomTransformLocked(ctx, groupResource, bindingName, ct)
		grTransformData.changes.removes = append(grTransformData.changes.removes, removes...)
		grTransformData.ctNames.Insert(ct.Name)
	}
	if len(ctAnys) > 1 { // This is not recommended
		logger.Error(nil, "Multiple CustomTransform objects apply to one GroupResource", "groupResource", groupResource, "names", grTransformData.ctNames)
	}
	ctc.grToTransformData[groupResource] = grTransformData
	return grTransformData.changes
}

// digestCustomTransformLocked digests one CustomTransform on behalf of one Binding.
// Caller asserts that grToTransformData does not have an entry for this GroupResource.
// Caller asserts that the ctc's mutex is locked.
func (ctc *customTransformCollection) digestCustomTransformLocked(ctx context.Context, groupResource metav1.GroupResource, bindingName string, ct *v1alpha1.CustomTransform) []jsonpath.Query {
	removes := ctc.parseRemovesAndUpdateStatus(ctx, ct)
	// Invalidate cache if ct.Spec changed its .Group or .Resource since last processed in this method
	oldSpec, had := ctc.ctNameToSpec[ct.Name]
	if had {
		oldGroupResource := ctSpecGroupResource(oldSpec)
		if oldGroupResource != groupResource { // ct has changed its GroupResource since last processed
			ctc.invalidateCacheEntryLocked(ctx, true, ct.Name, bindingName, oldGroupResource, "CustomTransformSpec .Group or .Resource changed", "newGroupResource", groupResource)
		} else {
			klog.FromContext(ctx).Error(nil, "Impossible condition: ctNameToSpec has an entry but grToTransformData does not and no change in GroupResource", "customTransformName", ct.Name, "groupResource", groupResource, "bindingName", bindingName)
		}
	}
	ctc.ctNameToSpec[ct.Name] = ct.Spec
	return removes
}

func ctSpecGroupResource(spec v1alpha1.CustomTransformSpec) metav1.GroupResource {
	return metav1.GroupResource{Group: spec.APIGroup, Resource: spec.Resource}
}

func (ctc *customTransformCollection) parseRemovesAndUpdateStatus(ctx context.Context, ct *v1alpha1.CustomTransform) (removes []jsonpath.Query) {
	logger := klog.FromContext(ctx)
	ctCopy := ct.DeepCopy()
	ctCopy.Status = v1alpha1.CustomTransformStatus{ObservedGeneration: ct.Generation}
	for idx, queryS := range ct.Spec.Remove {
		query, err := jsonpath.ParseQuery(queryS)
		if err != nil {
			ctCopy.Status.Errors = append(ctCopy.Status.Errors, fmt.Sprintf("Error in spec.remove[%d]: %s", idx, err.Error()))
		} else if len(query) == 0 {
			ctCopy.Status.Errors = append(ctCopy.Status.Errors, fmt.Sprintf("Invalid spec.remove[%d]: it identifies the whole object", idx))
		} else {
			removes = append(removes, query)
		}
	}
	ctEcho, err := ctc.client.UpdateStatus(ctx, ctCopy, metav1.UpdateOptions{FieldManager: ControllerName})
	if err != nil {
		logger.Error(err, "Failed to write status of CustomTransform", "name", ct.Name, "resourceVersion", ct.ResourceVersion, "status", ctCopy.Status)
	} else {
		logger.V(4).Info("Wrote status of CustomTransform", "name", ct.Name, "resourceVersion", ctEcho.ResourceVersion, "observedGeneration", ctCopy.Status.ObservedGeneration)
	}
	return
}

// invalidateCacheEntryLocked removes the cached entry for the given GroupResource.
// Caller asserts that this is being done because of some change to the CustomTransform having the given name.
// `shouldHave` asserts that ctc.ctNameToSpec has an entry for this name.
// triggerBindingName, if not empty, is the name of the Binding being processed when this change was noticed.
// reason and extraLogArgs go into the debug log statements.
// Caller asserts that the ctc's mutex is locked.
func (ctc *customTransformCollection) invalidateCacheEntryLocked(ctx context.Context, shouldHave bool, ctName, triggerBindingName string, oldGroupResource metav1.GroupResource, reason string, extraLogArgs ...any) {
	logger := klog.FromContext(ctx)
	oldGRTransformData, had := ctc.grToTransformData[oldGroupResource]
	if !had {
		if shouldHave {
			logger.Error(nil, "Impossible condition: ctc.ctNameToSpec has an entry for a CustomTransform but ctc.grToTransformData has no entry for the GroupResource", "customTransformName", ctName, "groupResource", oldGroupResource)
		}
		return
	}
	delete(ctc.grToTransformData, oldGroupResource)
	for bindingName := range oldGRTransformData.bindingsThatCare {
		if bindingName == triggerBindingName {
			continue
		}
		logger.V(5).Info("Enqueuing reference to Binding because "+reason, append(extraLogArgs, "bindingName", bindingName, "customTransformName", ctName, "oldGroupResource", oldGroupResource))
		ctc.enqueue(bindingName)
	}
	for ctName := range oldGRTransformData.ctNames {
		delete(ctc.ctNameToSpec, ctName)
	}
}

// NoteCustomTransform is the work that the customTransformCollection has to do
// in order to react to a notification of a create/update/delete of a CustomTransform.
// This method will invalidate the cache entry(s) for the given CustomTransform if
// it changed since its contents contributed to that cache entry.
func (ctc *customTransformCollection) NoteCustomTransform(ctx context.Context, name string, ct *v1alpha1.CustomTransform) {
	ctc.mutex.Lock()
	defer ctc.mutex.Unlock()
	oldSpec, hadSpec := ctc.ctNameToSpec[name]
	if ct == nil && !hadSpec { // ct is gone now and there is no cache entry relevant to ct
		return
	}
	var oldGroupResource, newGroupResource, theGroupResource metav1.GroupResource
	if hadSpec {
		oldGroupResource = ctSpecGroupResource(oldSpec)
		theGroupResource = oldGroupResource
	}
	if ct != nil {
		newGroupResource = ctSpecGroupResource(ct.Spec)
		theGroupResource = newGroupResource
	}
	if ct != nil && hadSpec &&
		oldGroupResource == newGroupResource &&
		sets.New(oldSpec.Remove...).Equal(sets.New(ct.Spec.Remove...)) {
		return // unchanged
	}
	if ct != nil && hadSpec && oldGroupResource != newGroupResource {
		// ct.Spec changed its GroupResource
		ctc.invalidateCacheEntryLocked(ctx, true, name, "", oldGroupResource, "CustomTransformSpec changed its GroupResource", "newGroupResource", newGroupResource)
	}
	ctc.invalidateCacheEntryLocked(ctx, false, name, "", theGroupResource, "CustomTransformSpec changed")
	delete(ctc.ctNameToSpec, name)
}

// SetBindingGroupResources updates the customTransformCollection with the knowledge of the full set of GroupResources that
// a given Binding depends on.
func (ctc *customTransformCollection) SetBindingGroupResources(bindingName string, newGroupResources sets.Set[metav1.GroupResource]) {
	ctc.mutex.Lock()
	defer ctc.mutex.Unlock()
	oldGroupResources := ctc.bindingNameToGroupResources[bindingName]

	// Remove Binding name from the set of those that depend on each GroupResource that is no longer relevant,
	// removing grToTransformData entries that would have an empty set of Binding name.
	for groupResource := range oldGroupResources {
		if newGroupResources.Has(groupResource) {
			continue
		}
		// This one is being removed
		if grTransformData, ok := ctc.grToTransformData[groupResource]; ok {
			grTransformData.bindingsThatCare.Delete(bindingName)
			// When the set goes empty, time to delete this data
			if grTransformData.bindingsThatCare.Len() == 0 {
				delete(ctc.grToTransformData, groupResource)
			}
		}
	}

	// Update bindingNameToGroupResources
	if len(newGroupResources) == 0 {
		delete(ctc.bindingNameToGroupResources, bindingName)
	} else {
		ctc.bindingNameToGroupResources[bindingName] = newGroupResources
	}
}
