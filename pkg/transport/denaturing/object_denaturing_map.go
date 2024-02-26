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

package denaturing

import (
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// cleanObjectSpecificsFunction is a function for cleaning fields from a specific object.
// The function cleans the specific fields in place (object is modified).
// If the object was retrieved using a lister, it's the caller responsibility
// to do a DeepCopy before calling this function.
type cleanObjectSpecificsFunction func(object *unstructured.Unstructured)

func NewObjectDenaturingMap() *ObjectDenaturingMap {
	serviceTypeMeta := v1.TypeMeta{Kind: "Service", APIVersion: "v1"}

	denaturingMap := map[string]cleanObjectSpecificsFunction{
		serviceTypeMeta.GroupVersionKind().String(): cleanServiceFields,
	}

	return &ObjectDenaturingMap{
		gvkToDenaturingFunc: denaturingMap,
	}
}

type ObjectDenaturingMap struct {
	gvkToDenaturingFunc map[string]cleanObjectSpecificsFunction // map from GVK as string to clean object function
}

func (denaturingMap *ObjectDenaturingMap) CleanObjectSpecifics(object *unstructured.Unstructured) {
	gvkAsString := object.GetObjectKind().GroupVersionKind().String()
	denaturingFunction, found := denaturingMap.gvkToDenaturingFunc[gvkAsString]
	if !found {
		return // if no denaturing function was defined for this gvk, do not clean any field
	}
	// otherwise, need to clean specific fields from this object
	denaturingFunction(object)
}
