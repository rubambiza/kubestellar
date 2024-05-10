/*
Copyright The KubeStellar Authors.

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

// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	rest "k8s.io/client-go/rest"
	testing "k8s.io/client-go/testing"

	v1alpha1 "github.com/kubestellar/kubestellar/pkg/generated/clientset/versioned/typed/control/v1alpha1"
)

type FakeControlV1alpha1 struct {
	*testing.Fake
}

func (c *FakeControlV1alpha1) Bindings() v1alpha1.BindingInterface {
	return &FakeBindings{c}
}

func (c *FakeControlV1alpha1) BindingPolicies() v1alpha1.BindingPolicyInterface {
	return &FakeBindingPolicies{c}
}

func (c *FakeControlV1alpha1) CombinedStatuses(namespace string) v1alpha1.CombinedStatusInterface {
	return &FakeCombinedStatuses{c, namespace}
}

func (c *FakeControlV1alpha1) CustomTransforms() v1alpha1.CustomTransformInterface {
	return &FakeCustomTransforms{c}
}

func (c *FakeControlV1alpha1) StatusCollectors(namespace string) v1alpha1.StatusCollectorInterface {
	return &FakeStatusCollectors{c, namespace}
}

// RESTClient returns a RESTClient that is used to communicate
// with API server by this client implementation.
func (c *FakeControlV1alpha1) RESTClient() rest.Interface {
	var ret *rest.RESTClient
	return ret
}
