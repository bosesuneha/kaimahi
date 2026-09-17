package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v6"
)

type azureCluster struct {
	Name              string `json:"name"`
	ResourceGroup     string `json:"resourceGroup"`
	Location          string `json:"location"`
	KubernetesVersion string `json:"kubernetesVersion"`
}

// Retain both credential and client so repeated discovery can reuse ARM's
// bearer-token and HTTP connection caches. Resource lists themselves stay fresh.
type azureSDKDiscovery struct {
	mu         sync.Mutex
	clients    map[string]*armcontainerservice.ManagedClustersClient
	credential func(string) (azcore.TokenCredential, error)
	options    *arm.ClientOptions
}

func newAzureSDKDiscovery() *azureSDKDiscovery {
	return &azureSDKDiscovery{credential: func(tenant string) (azcore.TokenCredential, error) {
		return azidentity.NewDefaultAzureCredential(&azidentity.DefaultAzureCredentialOptions{TenantID: tenant})
	}}
}

func (d *azureSDKDiscovery) client(subscription, tenant string) (*armcontainerservice.ManagedClustersClient, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	key := tenant + "/" + subscription
	if client := d.clients[key]; client != nil {
		return client, nil
	}
	cred, err := d.credential(tenant)
	if err != nil {
		return nil, err
	}
	client, err := armcontainerservice.NewManagedClustersClient(subscription, cred, d.options)
	if err != nil {
		return nil, err
	}
	if d.clients == nil {
		d.clients = map[string]*armcontainerservice.ManagedClustersClient{}
	}
	d.clients[key] = client
	return client, nil
}

func (d *azureSDKDiscovery) clusters(ctx context.Context, subscription, tenant string) ([]azureCluster, error) {
	if subscription == "" {
		return nil, fmt.Errorf("Azure subscription is required")
	}
	client, err := d.client(subscription, tenant)
	if err != nil {
		return nil, fmt.Errorf("initialize Azure SDK discovery: %w", err)
	}
	clusters := []azureCluster{}
	pager := client.NewListPager(nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list AKS clusters in subscription %s using Azure SDK: %w", subscription, err)
		}
		for _, cluster := range page.Value {
			if cluster == nil || cluster.ID == nil || cluster.Name == nil {
				return nil, fmt.Errorf("Azure returned a cluster without an identity")
			}
			id, err := arm.ParseResourceID(*cluster.ID)
			if err != nil || id.ResourceGroupName == "" {
				return nil, fmt.Errorf("Azure returned an invalid cluster resource ID")
			}
			entry := azureCluster{Name: *cluster.Name, ResourceGroup: id.ResourceGroupName}
			if cluster.Location != nil {
				entry.Location = *cluster.Location
			}
			if cluster.Properties != nil && cluster.Properties.KubernetesVersion != nil {
				entry.KubernetesVersion = *cluster.Properties.KubernetesVersion
			}
			clusters = append(clusters, entry)
		}
	}
	return clusters, nil
}

func (b *orkaChatBackend) liftClusters(ctx context.Context, target chatLiftTarget) ([]byte, error) {
	if b.app.azureDiscoveryMode != "sdk" {
		return b.liftAzureFetch(ctx, aksListArgs(target)...)
	}
	if b.azureDiscovery == nil {
		b.azureDiscovery = newAzureSDKDiscovery()
	}
	return b.liftLoading(ctx, "Fetching AKS clusters (Go SDK) · subscription:"+target.Subscription, func(ctx context.Context) ([]byte, error) {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		clusters, err := b.azureDiscovery.clusters(ctx, target.Subscription, target.Tenant)
		if err != nil {
			return nil, err
		}
		return json.Marshal(clusters)
	})
}
