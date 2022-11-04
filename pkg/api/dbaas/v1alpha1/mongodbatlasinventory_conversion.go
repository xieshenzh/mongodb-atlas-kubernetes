/*
Copyright 2022.
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

package v1alpha1

import (
	"sigs.k8s.io/controller-runtime/pkg/conversion"

	dbaasv1alpha1 "github.com/RHEcosystemAppEng/dbaas-operator/api/v1alpha1"
	dbaasv1alpha2 "github.com/RHEcosystemAppEng/dbaas-operator/api/v1alpha2"
	"github.com/mongodb/mongodb-atlas-kubernetes/pkg/api/dbaas/v1alpha2"
)

// ConvertTo converts this MongoDBAtlasInventory to the Hub version (v1alpha2).
func (src *MongoDBAtlasInventory) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1alpha2.MongoDBAtlasInventory)

	if src.Spec.CredentialsRef != nil {
		dst.Spec.CredentialsRef = &dbaasv1alpha2.LocalObjectReference{
			Name: src.Spec.CredentialsRef.Name,
		}
	}

	dst.ObjectMeta = src.ObjectMeta

	dst.Status.Conditions = src.Status.Conditions
	if src.Status.Instances != nil {
		var services []dbaasv1alpha2.DatabaseService
		for _, instance := range src.Status.Instances {
			services = append(services, dbaasv1alpha2.DatabaseService{
				ServiceID:   instance.InstanceID,
				ServiceName: instance.Name,
				ServiceInfo: instance.InstanceInfo,
				ServiceType: dbaasv1alpha2.InstanceDatabaseService,
			})
		}
		dst.Status.DatabaseServices = services
	}

	return nil
}

// ConvertFrom converts from the Hub version (v1alpha2) to this version.
func (dst *MongoDBAtlasInventory) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1alpha2.MongoDBAtlasInventory)

	if src.Spec.CredentialsRef != nil {
		dst.Spec.CredentialsRef = &dbaasv1alpha1.LocalObjectReference{
			Name: src.Spec.CredentialsRef.Name,
		}
	}

	dst.ObjectMeta = src.ObjectMeta

	dst.Status.Conditions = src.Status.Conditions
	if src.Status.DatabaseServices != nil {
		var instances []dbaasv1alpha1.Instance
		for _, service := range src.Status.DatabaseServices {
			if service.ServiceType == dbaasv1alpha2.InstanceDatabaseService {
				instances = append(instances, dbaasv1alpha1.Instance{
					InstanceID:   service.ServiceID,
					Name:         service.ServiceName,
					InstanceInfo: service.ServiceInfo,
				})
			}
		}
		dst.Status.Instances = instances
	}

	return nil
}
