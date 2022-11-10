package atlasinventory

import (
	"context"
	"net/http"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"go.mongodb.org/atlas/mongodbatlas"

	dbaasv1alpha1 "github.com/RHEcosystemAppEng/dbaas-operator/api/v1alpha1"
	dbaasv1alpha2 "github.com/RHEcosystemAppEng/dbaas-operator/api/v1alpha2"
	"github.com/mongodb/mongodb-atlas-kubernetes/pkg/api/dbaas/v1alpha1"
	"github.com/mongodb/mongodb-atlas-kubernetes/pkg/controller/workflow"
)

// discoverInstances query atlas and return list of instances found
func discoverInstances(atlasClient *mongodbatlas.Client) ([]dbaasv1alpha2.DatabaseService, workflow.Result) {
	// Try to find the service
	projects, response, err := atlasClient.Projects.GetAllProjects(context.Background(), &mongodbatlas.ListOptions{})
	if err != nil {
		return nil, workflow.Terminate(getReasonFromResponse(response), err.Error())
	}
	processed := map[string]bool{}
	instanceList := []dbaasv1alpha2.DatabaseService{}
	for _, p := range projects.Results {
		if _, ok := processed[p.ID]; ok {
			// This project ID has been processed. Move on to next.
			continue
		}
		clusters, response, err := atlasClient.Clusters.List(context.Background(), p.ID, &mongodbatlas.ListOptions{})
		if err != nil {
			return nil, workflow.Terminate(getReasonFromResponse(response), err.Error())
		}
		for _, cluster := range clusters {
			instanceList = append(instanceList, GetInstance(*p, cluster))
		}
		processed[p.ID] = true
	}
	return instanceList, workflow.OK()
}

func getReasonFromResponse(response *mongodbatlas.Response) workflow.ConditionReason {
	reason := workflow.MongoDBAtlasInventoryBackendError
	if response == nil {
		return reason
	}
	if response.StatusCode == http.StatusUnauthorized {
		reason = workflow.MongoDBAtlasInventoryAuthenticationError
	} else if response.StatusCode == http.StatusBadGateway || response.StatusCode == http.StatusServiceUnavailable {
		reason = workflow.MongoDBAtlasInventoryEndpointUnreachable
	}
	return reason
}

// GetClusterInfo query atlas for the cluster and return the relevant data required by DBaaS Operator
func GetClusterInfo(atlasClient *mongodbatlas.Client, projectName, clusterName string) (*dbaasv1alpha2.DatabaseService, workflow.Result) {
	// Try to find the service
	project, response, err := atlasClient.Projects.GetOneProjectByName(context.Background(), projectName)
	if err != nil {
		return nil, workflow.Terminate(getReasonFromResponse(response), err.Error())
	}
	cluster, response, err := atlasClient.Clusters.Get(context.Background(), project.ID, clusterName)
	if err != nil {
		return nil, workflow.Terminate(getReasonFromResponse(response), err.Error())
	}
	instance := GetInstance(*project, *cluster)
	return &instance, workflow.OK()
}

// GetInstance returns instance info as required by DBaaS Operator
func GetInstance(project mongodbatlas.Project, cluster mongodbatlas.Cluster) dbaasv1alpha2.DatabaseService {
	// Convert state names to "Creating", "Ready", "Deleting", "Deleted" etc.
	// Pending - provisioning not yet started
	// Creating - provisioning in progress
	// Updating - cluster updating in progress
	// Deleting - cluster deletion in progress
	// Deleted - cluster has been deleted
	// Ready - cluster provisioning complete
	caser := cases.Title(language.AmericanEnglish)
	phase := parsePhase(caser.String(strings.ToLower(cluster.StateName)))
	provider := cluster.ProviderSettings.BackingProviderName
	if len(provider) == 0 {
		provider = cluster.ProviderSettings.ProviderName
	}
	return dbaasv1alpha2.DatabaseService{
		ServiceID:   cluster.ID,
		ServiceName: cluster.Name,
		ServiceType: dbaasv1alpha2.InstanceDatabaseService,
		ServiceInfo: map[string]string{
			v1alpha1.InstanceSizeNameKey:             cluster.ProviderSettings.InstanceSizeName,
			v1alpha1.CloudProviderKey:                provider,
			v1alpha1.CloudRegionKey:                  cluster.ProviderSettings.RegionName,
			v1alpha1.ProjectIDKey:                    project.ID,
			v1alpha1.ProjectNameKey:                  project.Name,
			v1alpha1.ConnectionStringsStandardSrvKey: cluster.ConnectionStrings.StandardSrv,
			v1alpha1.ProvisionPhaseKey:               string(phase),
		},
	}
}

func parsePhase(state string) dbaasv1alpha1.DBaasInstancePhase {
	switch state {
	case "Pending":
		return dbaasv1alpha1.InstancePhasePending
	case "Creating":
		return dbaasv1alpha1.InstancePhaseCreating
	case "Updating":
		return dbaasv1alpha1.InstancePhaseUpdating
	case "Deleting":
		return dbaasv1alpha1.InstancePhaseDeleting
	case "Deleted":
		return dbaasv1alpha1.InstancePhaseDeleted
	case "Ready", "Idle":
		return dbaasv1alpha1.InstancePhaseReady
	default:
		return dbaasv1alpha1.InstancePhaseUnknown
	}
}
