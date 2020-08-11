/*
Copyright 2020 The Kubernetes Authors.

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

package bastionhosts

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/services/network/mgmt/2019-06-01/network"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/pkg/errors"

	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1alpha3"
	azure "sigs.k8s.io/cluster-api-provider-azure/cloud"
	"sigs.k8s.io/cluster-api-provider-azure/cloud/converters"
)

// Reconcile gets/creates/updates a bastion host.
func (s *Service) Reconcile(ctx context.Context) error {
	for _, bastionSpec := range s.Scope.BastionSpecs() {
		s.Scope.V(2).Info("getting subnet in vnet", "subnet", bastionSpec.SubnetName, "vNet", bastionSpec.VNetName)
		subnet, err := s.SubnetsClient.Get(ctx, s.Scope.ResourceGroup(), bastionSpec.VNetName, bastionSpec.SubnetName)
		if err != nil {
			return errors.Wrap(err, "failed to get subnet")
		}
		s.Scope.V(2).Info("successfully got subnet in vnet", "subnet", bastionSpec.SubnetName, "vNet", bastionSpec.VNetName)

		s.Scope.V(2).Info("checking if public ip exist otherwise will try to create", "publicIP", bastionSpec.PublicIPName)
		publicIP := network.PublicIPAddress{}
		publicIP, err = s.PublicIPsClient.Get(ctx, s.Scope.ResourceGroup(), bastionSpec.PublicIPName)
		if err != nil && azure.ResourceNotFound(err) {
			iperr := s.createBastionPublicIP(ctx, bastionSpec.PublicIPName)
			if iperr != nil {
				return errors.Wrap(iperr, "failed to create bastion publicIP")
			}
			var errPublicIP error
			publicIP, errPublicIP = s.PublicIPsClient.Get(ctx, s.Scope.ResourceGroup(), bastionSpec.PublicIPName)
			if errPublicIP != nil {
				return errors.Wrap(errPublicIP, "failed to get created publicIP")
			}
		} else if err != nil {
			return errors.Wrap(err, "failed to get existing publicIP")
		}
		s.Scope.V(2).Info("successfully got public ip", "publicIP", bastionSpec.PublicIPName)

		s.Scope.V(2).Info("creating bastion host", "bastion", bastionSpec.Name)
		bastionHostIPConfigName := fmt.Sprintf("%s-%s", bastionSpec.Name, "bastionIP")
		err = s.Client.CreateOrUpdate(
			ctx,
			s.Scope.ResourceGroup(),
			bastionSpec.Name,
			network.BastionHost{
				Name:     to.StringPtr(bastionSpec.Name),
				Location: to.StringPtr(s.Scope.Location()),
				Tags: converters.TagsToMap(infrav1.Build(infrav1.BuildParams{
					ClusterName: s.Scope.ClusterName(),
					Lifecycle:   infrav1.ResourceLifecycleOwned,
					Name:        to.StringPtr(bastionSpec.Name),
					Role:        to.StringPtr("Bastion"),
				})),
				BastionHostPropertiesFormat: &network.BastionHostPropertiesFormat{
					DNSName: to.StringPtr(fmt.Sprintf("%s-bastion", strings.ToLower(bastionSpec.Name))),
					IPConfigurations: &[]network.BastionHostIPConfiguration{
						{
							Name: to.StringPtr(bastionHostIPConfigName),
							BastionHostIPConfigurationPropertiesFormat: &network.BastionHostIPConfigurationPropertiesFormat{
								Subnet: &network.SubResource{
									ID: subnet.ID,
								},
								PublicIPAddress: &network.SubResource{
									ID: publicIP.ID,
								},
								PrivateIPAllocationMethod: network.Static,
							},
						},
					},
				},
			},
		)
		if err != nil {
			return errors.Wrap(err, "cannot create bastion host")
		}

		s.Scope.V(2).Info("successfully created bastion host", "bastion", bastionSpec.Name)
	}
	return nil
}

// Delete deletes the bastion host with the provided scope.
func (s *Service) Delete(ctx context.Context) error {
	for _, bastionSpec := range s.Scope.BastionSpecs() {

		s.Scope.V(2).Info("deleting bastion host", "bastion", bastionSpec.Name)

		err := s.Client.Delete(ctx, s.Scope.ResourceGroup(), bastionSpec.Name)
		if err != nil && azure.ResourceNotFound(err) {
			// already deleted
			return nil
		}
		if err != nil {
			return errors.Wrapf(err, "failed to delete Bastion Host %s in resource group %s", bastionSpec.Name, s.Scope.ResourceGroup())
		}

		s.Scope.V(2).Info("successfully deleted bastion host", "bastion", bastionSpec.Name)
	}
	return nil
}

func (s *Service) createBastionPublicIP(ctx context.Context, ipName string) error {
	s.Scope.V(2).Info("creating bastion public IP", "public IP", ipName)
	return s.PublicIPsClient.CreateOrUpdate(
		ctx,
		s.Scope.ResourceGroup(),
		ipName,
		network.PublicIPAddress{
			Sku:      &network.PublicIPAddressSku{Name: network.PublicIPAddressSkuNameStandard},
			Name:     to.StringPtr(ipName),
			Location: to.StringPtr(s.Scope.Location()),
			PublicIPAddressPropertiesFormat: &network.PublicIPAddressPropertiesFormat{
				PublicIPAddressVersion:   network.IPv4,
				PublicIPAllocationMethod: network.Static,
				DNSSettings: &network.PublicIPAddressDNSSettings{
					DomainNameLabel: to.StringPtr(strings.ToLower(ipName)),
				},
			},
		},
	)
}
