/*
Copyright 2023 The KubeStellar Authors.

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

package util

import (
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/kubestellar/kubestellar/api/control/v1alpha1"
)

const (
	// BindingPolicyLabelSingletonStatusKey is the key for the singleton status reporting
	// requirement. The value should be the bindingpolicy that is selecting the labeled object.
	BindingPolicyLabelSingletonStatusKey = "managed-by.kubestellar.io/singletonstatus"
)

func GetBindingPolicyListerKey() string {
	return KeyForGroupVersionKind(v1alpha1.GroupVersion.Group,
		v1alpha1.GroupVersion.Version, BindingPolicyKind)
}

func GetBindingListerKey() string {
	return KeyForGroupVersionKind(v1alpha1.GroupVersion.Group,
		v1alpha1.GroupVersion.Version, BindingKind)
}

type Label struct {
	Key   string
	Value string
}

func SplitLabelKeyAndValue(keyvalue string) (Label, error) {
	label := Label{}
	parts := strings.Split(keyvalue, "=")
	if len(parts) != 2 {
		return label, fmt.Errorf("invalid key=value label: %s", keyvalue)
	}
	label.Key = parts[0]
	label.Value = parts[1]
	return label, nil
}

func SelectorsMatchLabels(selectors []metav1.LabelSelector, labelsSet labels.Set) (bool, error) {
	matches := true
	for _, selectorApi := range selectors {
		selector, err := metav1.LabelSelectorAsSelector(&selectorApi)
		if err != nil {
			return false, err
		}
		if !selector.Matches(labelsSet) {
			matches = false
			break
		}
	}
	return matches, nil
}
