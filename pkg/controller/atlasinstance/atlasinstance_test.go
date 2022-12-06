// Copyright 2022 MongoDB Inc
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package atlasinstance

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	dbaasv1alpha1 "github.com/RHEcosystemAppEng/dbaas-operator/api/v1alpha1"
	dbaasv1beta1 "github.com/RHEcosystemAppEng/dbaas-operator/api/v1beta1"
	"github.com/fgrosse/zaptest"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"go.mongodb.org/atlas/mongodbatlas"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dbaas "github.com/mongodb/mongodb-atlas-kubernetes/pkg/api/dbaas"
	v1 "github.com/mongodb/mongodb-atlas-kubernetes/pkg/api/v1"
	"github.com/mongodb/mongodb-atlas-kubernetes/pkg/api/v1/common"
	status "github.com/mongodb/mongodb-atlas-kubernetes/pkg/api/v1/status"
	"github.com/mongodb/mongodb-atlas-kubernetes/pkg/controller/watch"
)

func TestGetInstanceData(t *testing.T) {
	log := zaptest.Logger(t).Sugar()
	testCase := map[string]struct {
		deploymentName      string
		projectName         string
		providerName        string
		regionName          string
		instanceSizeName    string
		expProviderName     string
		expRegionName       string
		expInstanceSizeName string
		expErrMsg           string
	}{
		"Nominal": {
			deploymentName:      "myDeployment",
			projectName:         "myProject",
			providerName:        "GCP",
			regionName:          "GCP_REGION",
			instanceSizeName:    "M10",
			expProviderName:     "GCP",
			expRegionName:       "GCP_REGION",
			expInstanceSizeName: "M10",
			expErrMsg:           "",
		},
		"MissingDeploymentName": {
			deploymentName:      "",
			projectName:         "myProject",
			providerName:        "GCP",
			regionName:          "GCP_REGION",
			instanceSizeName:    "M10",
			expProviderName:     "GCP",
			expRegionName:       "GCP_REGION",
			expInstanceSizeName: "M10",
			expErrMsg:           "missing clusterName",
		},
		"MissingProjectName": {
			deploymentName:      "myDeployment",
			projectName:         "",
			providerName:        "GCP",
			regionName:          "GCP_REGION",
			instanceSizeName:    "M10",
			expProviderName:     "GCP",
			expRegionName:       "GCP_REGION",
			expInstanceSizeName: "M10",
			expErrMsg:           "missing projectName",
		},
		"UseDefaultProvider": {
			deploymentName:      "myDeployment",
			projectName:         "myProject",
			providerName:        "",
			regionName:          "AWS_REGION",
			instanceSizeName:    "M10",
			expProviderName:     "AWS",
			expRegionName:       "AWS_REGION",
			expInstanceSizeName: "M10",
			expErrMsg:           "",
		},
		"UseDefaultRegion": {
			deploymentName:      "myDeployment",
			projectName:         "myProject",
			providerName:        "AWS",
			regionName:          "",
			instanceSizeName:    "M10",
			expProviderName:     "AWS",
			expRegionName:       "US_EAST_1",
			expInstanceSizeName: "M10",
			expErrMsg:           "",
		},
		"UseDefaultInstanceSizeName": {
			deploymentName:      "myDeployment",
			projectName:         "myProject",
			providerName:        "AWS",
			regionName:          "US_EAST_1",
			instanceSizeName:    "",
			expProviderName:     "AWS",
			expRegionName:       "US_EAST_1",
			expInstanceSizeName: "M0",
			expErrMsg:           "",
		},
	}

	for tcName, tc := range testCase {
		t.Run(tcName, func(t *testing.T) {
			instance := &dbaas.MongoDBAtlasInstance{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "dbaas.redhat.com/v1alpha1",
					Kind:       "MongoDBAtlasInstance",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("instance-%s", tcName),
					Namespace: "dbaas-operator",
				},
				Spec: dbaasv1alpha1.DBaaSInstanceSpec{
					InventoryRef: dbaasv1alpha1.NamespacedName{
						Name:      fmt.Sprintf("inventory-%s", tcName),
						Namespace: "dbaas-operator",
					},
					Name:          tc.deploymentName,
					CloudProvider: tc.providerName,
					CloudRegion:   tc.regionName,
					OtherInstanceParams: map[string]string{
						"projectName":      tc.projectName,
						"instanceSizeName": tc.instanceSizeName,
					},
				},
			}

			expected := &InstanceData{
				ProjectName:      tc.projectName,
				ClusterName:      tc.deploymentName,
				ProviderName:     tc.expProviderName,
				RegionName:       tc.expRegionName,
				InstanceSizeName: tc.expInstanceSizeName,
			}
			res, err := getInstanceData(log, instance)
			if len(tc.expErrMsg) == 0 {
				assert.NoError(t, err)
				assert.True(t, reflect.DeepEqual(expected, res))
			} else {
				assert.Equal(t, err.Error(), tc.expErrMsg)
			}
		})
	}
}

const (
	// baseURLPath is a non-empty Client.BaseURL path to use during tests,
	// to ensure relative URLs are used for all endpoints.
	baseURLPath = "/api-v1"
)

// setupMockAltasServer sets up a test HTTP server along with a mongodbatlas.Client that is
// configured to talk to that test server. Tests should register handlers on
// mux which provide mock responses for the API method being tested.
func setupMockAltasServer(t *testing.T) (client *mongodbatlas.Client, teardown func()) {
	// mux is the HTTP request multiplexer used with the test server.
	router := mux.NewRouter()

	// We want to ensure that tests catch mistakes where the endpoint URL is
	// specified as absolute rather than relative. It only makes a difference
	// when there's a non-empty base URL path. So, use that.
	apiHandler := http.NewServeMux()
	apiHandler.Handle(baseURLPath+"/", http.StripPrefix(baseURLPath, router))
	router.HandleFunc("/api/atlas/v1.0/groups/byName/{group-name}", func(w http.ResponseWriter, r *http.Request) {
		if m := http.MethodGet; m != r.Method {
			t.Errorf("Request method = %v, expected %v", r.Method, m)
		}
		vars := mux.Vars(r)
		groupName, ok := vars["group-name"]
		if !ok {
			fmt.Fprint(w, "group-id is missing in parameters")
			return
		}
		var data []byte
		var err error
		data, err = ioutil.ReadFile(fmt.Sprintf("../../../test/e2e/data/atlasprojectget_%s.json", groupName))
		if err == nil {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, string(data))
		} else {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, "{\"detail\":\"The current user is not in the group, or the group does not exist.\",\"error\":401,\"errorCode\":\"NOT_IN_GROUP\",\"parameters\":[],\"reason\":\"Unauthorized\"}")
		}
	}).Methods(http.MethodGet)

	router.HandleFunc("/api/atlas/v1.0/groups/{group-id}/clusters/{deployment-name}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		groupID, ok := vars["group-id"]
		if !ok {
			fmt.Fprint(w, "group-id is missing in parameters")
		}
		deploymentName, ok := vars["deployment-name"]
		if !ok {
			fmt.Fprint(w, "deployment-name is missing in parameters")
		}
		data, err := ioutil.ReadFile(fmt.Sprintf("../../../test/e2e/data/atlasdeploymentget_%s_%s.json", groupID, deploymentName))
		if err == nil {
			assert.NoError(t, err)
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, string(data))
		} else {
			w.WriteHeader(http.StatusNotFound)
			f := "{\"detail\":\"No deployment named %s exists in group %s.\",\"error\":404,\"errorCode\":\"CLUSTER_NOT_FOUND\",\"parameters\":[\"%s\",\"groupid123\"],\"reason\":\"Not Found\"}"
			fmt.Fprintf(w, f, deploymentName, groupID, deploymentName)
		}
	}).Methods(http.MethodGet)

	// server is a test HTTP server used to provide mock API responses.
	server := httptest.NewServer(apiHandler)

	// client is the Atlas client being tested and is
	// configured to use test server.
	client = mongodbatlas.NewClient(nil)
	u, _ := url.Parse(server.URL + baseURLPath + "/")
	client.BaseURL = u

	return client, server.Close
}

func TestSetInstanceStatusWithDeploymentInfo(t *testing.T) {
	atlasClient, teardown := setupMockAltasServer(t)
	defer teardown()

	namespace := "default"
	testCase := map[string]struct {
		deploymentName         string
		projectName            string
		deploymentCRStatus     corev1.ConditionStatus
		expErrMsg              string
		expPhase               dbaasv1alpha1.DBaasInstancePhase
		expStatus              string
		expStateChangedInAtlas bool
	}{
		"DeploymentCreatingNorminal": {
			deploymentName:         "mydeploymentcreating",
			projectName:            "myproject",
			deploymentCRStatus:     corev1.ConditionFalse,
			expErrMsg:              "",
			expPhase:               dbaasv1alpha1.InstancePhaseCreating,
			expStatus:              "False",
			expStateChangedInAtlas: false,
		},
		"DeploymentCreatingChangedInAtlas": {
			deploymentName:         "mydeploymentcreating",
			projectName:            "myproject",
			deploymentCRStatus:     corev1.ConditionTrue,
			expErrMsg:              "",
			expPhase:               dbaasv1alpha1.InstancePhaseCreating,
			expStatus:              "False",
			expStateChangedInAtlas: true,
		},
		"DeploymentReady": {
			deploymentName:         "mydeploymentready",
			projectName:            "myproject",
			deploymentCRStatus:     corev1.ConditionTrue,
			expErrMsg:              "",
			expPhase:               dbaasv1alpha1.InstancePhaseReady,
			expStatus:              "True",
			expStateChangedInAtlas: false,
		},
		"InvalidProject": {
			deploymentName: "mydeploymentready",
			projectName:    "myproject-invalid",
			expErrMsg:      "NOT_IN_GROUP",
		},
	}
	for tcName, tc := range testCase {
		t.Run(tcName, func(t *testing.T) {
			atlasDeployment := &v1.AtlasDeployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-deployment-free",
					Namespace: namespace,
				},
				Spec: v1.AtlasDeploymentSpec{
					Project: common.ResourceRefNamespaced{
						Name:      "my-atlas-project-free",
						Namespace: namespace,
					},
					DeploymentSpec: &v1.DeploymentSpec{
						Name: tc.deploymentName,
						ProviderSettings: &v1.ProviderSettingsSpec{
							BackingProviderName: "AWS",
							InstanceSizeName:    "M0",
							ProviderName:        "TENANT",
							RegionName:          "US_EAST_1",
						},
					},
				},
				Status: status.AtlasDeploymentStatus{
					Common: status.Common{
						Conditions: []status.Condition{
							{
								Type:               status.ConditionType("Ready"),
								Status:             tc.deploymentCRStatus,
								LastTransitionTime: metav1.Now(),
							},
							{
								Type:               status.ConditionType("DeploymentReady"),
								Status:             tc.deploymentCRStatus,
								LastTransitionTime: metav1.Now(),
							},
						},
					},
				},
			}
			inst := &dbaas.MongoDBAtlasInstance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-instance",
					Namespace: namespace,
				},
				Spec: dbaasv1alpha1.DBaaSInstanceSpec{
					InventoryRef: dbaasv1alpha1.NamespacedName{
						Name:      "my-inventory",
						Namespace: namespace,
					},
					Name: tc.deploymentName,
					OtherInstanceParams: map[string]string{
						"projectName": tc.projectName,
					},
				},
			}
			stateChangedInAtlas, result := setInstanceStatusWithDeploymentInfo(atlasClient, inst, atlasDeployment, tc.projectName)
			if len(tc.expErrMsg) == 0 {
				cond := dbaas.GetInstanceCondition(inst, dbaasv1alpha1.DBaaSInstanceProviderSyncType)
				assert.NotNil(t, cond)
				assert.True(t, result.IsOk())
				assert.Equal(t, inst.Status.Phase, tc.expPhase)
				assert.Equal(t, string(cond.Status), tc.expStatus)
				assert.Equal(t, stateChangedInAtlas, tc.expStateChangedInAtlas)
			} else {
				assert.Contains(t, result.Message(), tc.expErrMsg)
			}
		})
	}
}

func TestAtlasInstanceReconcile(t *testing.T) {
	atlasClient, teardown := setupMockAltasServer(t)
	defer teardown()

	s := scheme.Scheme
	utilruntime.Must(scheme.AddToScheme(s))
	utilruntime.Must(v1.AddToScheme(s))
	utilruntime.Must(dbaas.AddToScheme(s))
	client := fake.NewClientBuilder().WithScheme(s).Build()
	logger := zaptest.Logger(t)

	// Create a MongoDBAtlasInstanceReconciler object with the scheme and fake client.
	r := &MongoDBAtlasInstanceReconciler{
		Client:          client,
		AtlasClient:     atlasClient,
		Scheme:          s,
		Log:             logger.Sugar(),
		ResourceWatcher: watch.NewResourceWatcher(),
	}

	tcName := "mytest"
	deploymentName := "mydeploymentnew"
	projectName := "myproject"
	expectedPhase := dbaasv1alpha1.InstancePhasePending
	expectedErrString := "CLUSTER_NOT_FOUND"
	expectedRequeue := true
	inventory := &dbaas.MongoDBAtlasInventory{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "dbaas.redhat.com/v1alpha1",
			Kind:       "MongoDBAtlasInventory",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("inventory-%s", tcName),
			Namespace: "dbaas-operator",
		},
		Spec: dbaasv1beta1.DBaaSInventorySpec{
			CredentialsRef: &dbaasv1beta1.LocalObjectReference{
				Name: fmt.Sprintf("secret-%s", tcName),
			},
		},
	}
	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind: "Opaque",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("secret-%s", tcName),
			Namespace: "dbaas-operator",
			Labels: map[string]string{
				"atlas.mongodb.com/type": "credentials",
			},
		},
		Data: map[string][]byte{
			"orgId":         []byte("testorgid"),
			"privateApiKey": []byte("testprivatekey"),
			"publicApiKey":  []byte("testpublickey"),
		},
	}

	instance := &dbaas.MongoDBAtlasInstance{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "dbaas.redhat.com/v1alpha1",
			Kind:       "MongoDBAtlasInstance",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("instance-%s", tcName),
			Namespace: "dbaas-operator",
		},
		Spec: dbaasv1alpha1.DBaaSInstanceSpec{
			Name: deploymentName,
			InventoryRef: dbaasv1alpha1.NamespacedName{
				Name:      inventory.Name,
				Namespace: inventory.Namespace,
			},
			OtherInstanceParams: map[string]string{
				"projectName": projectName,
			},
		},
	}
	err := client.Create(context.Background(), secret)
	assert.NoError(t, err)
	err = client.Create(context.Background(), inventory)
	assert.NoError(t, err)
	err = client.Create(context.Background(), instance)
	assert.NoError(t, err)

	// Mock request to simulate Reconcile() being called on an event for a
	// watched resource .
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      instance.Name,
			Namespace: instance.Namespace,
		},
	}
	res, err := r.Reconcile(context.Background(), req)
	if err != nil {
		assert.Contains(t, err.Error(), expectedErrString)
	} else {
		assert.Equal(t, expectedRequeue, res.Requeue)
	}
	instanceUpdated := &dbaas.MongoDBAtlasInstance{}
	err = client.Get(context.Background(),
		types.NamespacedName{
			Name:      instance.Name,
			Namespace: instance.Namespace,
		}, instanceUpdated)
	assert.NoError(t, err)
	assert.Equal(t, expectedPhase, instanceUpdated.Status.Phase)

	// After an instance is deleted, the corresponding atlas project should be deleted
	delEvent := event.DeleteEvent{Object: instance}
	err = r.Delete(delEvent)
	assert.NoError(t, err)
	atlasProject, err := r.getAtlasProject(context.Background(), instance)
	assert.NoError(t, err)
	assert.Nil(t, atlasProject)
}
