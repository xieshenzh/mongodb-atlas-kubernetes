package cloud

import (
	"fmt"
	"os"
	"path"

	"github.com/mongodb/mongodb-atlas-kubernetes/pkg/api/v1/status"
	"github.com/mongodb/mongodb-atlas-kubernetes/test/e2e/api/azure"
	"github.com/mongodb/mongodb-atlas-kubernetes/test/e2e/config"
)

type azureAction struct{}

var (
	// TODO get from Azure
	resourceGroup = "svet-test"
	vpc           = "svet-test-vpc"
	subnetName    = "default"
)

func (azureAction *azureAction) createPrivateEndpoint(pe status.ProjectPrivateEndpoint, privatelinkName string) (string, string, error) {
	session, err := azure.SessionAzure(os.Getenv("AZURE_SUBSCRIPTION_ID"), config.TagName)
	if err != nil {
		return "", "", err
	}
	err = session.DisableNetworkPolicies(resourceGroup, vpc, subnetName)
	if err != nil {
		return "", "", err
	}
	id, ip, err := session.CreatePrivateEndpoint(pe.Region, resourceGroup, privatelinkName, pe.ServiceResourceID)
	if err != nil {
		return "", "", err
	}
	return id, ip, nil
}

func (azureAction *azureAction) deletePrivateEndpoint(pe status.ProjectPrivateEndpoint, privatelinkName string) error {
	session, err := azure.SessionAzure(os.Getenv("AZURE_SUBSCRIPTION_ID"), config.TagName)
	if err != nil {
		return err
	}
	err = session.DeletePrivateEndpoint(resourceGroup, path.Base(privatelinkName))
	return err
}

func (azureAction *azureAction) statusPrivateEndpointPending(region, privatelinkName string) bool {
	session, err := azure.SessionAzure(os.Getenv("AZURE_SUBSCRIPTION_ID"), config.TagName)
	if err != nil {
		return false
	}
	status, err := session.GetPrivateEndpointStatus(resourceGroup, path.Base(privatelinkName))
	if err != nil {
		fmt.Print(err)
		return false
	}
	return (status == "Pending")
}

func (azureAction *azureAction) statusPrivateEndpointAvailable(region, privatelinkName string) bool {
	session, err := azure.SessionAzure(os.Getenv("AZURE_SUBSCRIPTION_ID"), config.TagName)
	if err != nil {
		fmt.Print(err)
		return false
	}
	status, err := session.GetPrivateEndpointStatus(resourceGroup, path.Base(privatelinkName))
	if err != nil {
		fmt.Print(err)
		return false
	}
	return (status == "Approved")
}
