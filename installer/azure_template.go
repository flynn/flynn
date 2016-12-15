package installer

import (
	"errors"
	"reflect"
	"text/template"
)

var azureTemplate = template.Must(template.New("azure_template.json").Funcs(template.FuncMap{
	"notLast": func(v interface{}, i int) (bool, error) {
		rv := reflect.ValueOf(v)
		if rv.Kind() != reflect.Slice {
			return false, errors.New("not a slice")
		}
		return i < rv.Len()-1, nil
	},
}).Parse(`
{
  "$schema": "http://schema.management.azure.com/schemas/2015-01-01/deploymentTemplate.json#",
  "contentVersion": "1.0.0",
  "parameters": {
    "Location": {
      "type": "string"
    },
    "VirtualMachineSize": {
      "type": "string"
    },
    "ClusterID": {
      "type": "string"
    },
    "StorageAccountName": {
      "type": "string"
    },
    "NumInstances": {
      "type": "int"
    },
    "VirtualMachineUser": {
      "type": "string"
    },
    "SSHPublicKey": {
      "type": "string"
    }
  },
  "variables": {
    "StorageAccountName": "[parameters('StorageAccountName')]",
    "StorageAccountType": "Standard_LRS",
    "PublicIPName": "[concat(parameters('ClusterID'), 'publicIP')]",
    "PublicIPAllocationMethod": "Dynamic",
    "PrivateIPAllocationMethod": "Dynamic",
    "VirtualNetworkName": "[concat(parameters('ClusterID'), 'vnet')]",
    "VirtualNetworkID": "[resourceId('Microsoft.Network/virtualNetworks', variables('VirtualNetworkName'))]",
    "NetworkSecurityGroupName": "[concat(parameters('ClusterID'), 'sg')]",
    "SubnetName": "[concat(parameters('ClusterID'), 'subnet')]",
    "SubnetRef": "[concat(variables('VirtualNetworkID'), '/subnets/', variables('SubnetName'))]",
    "NetworkInterfaceName": "[concat(parameters('ClusterID'), 'nic')]",
    "AddressPrefix": "10.0.0.0/16",
    "SubnetPrefix": "10.0.0.0/24",
    "VirtualMachineName": "[concat(parameters('ClusterID'), 'vm')]",
    "AuthorizedKeysPath": "[concat('/home/', parameters('VirtualMachineUser'), '/.ssh/authorized_keys')]"
  },
  "resources": [
    {
      "type": "Microsoft.Storage/storageAccounts",
      "name": "[variables('StorageAccountName')]",
      "location": "[parameters('Location')]",
      "apiVersion": "2015-05-01-preview",
      "properties": {
        "accountType": "[variables('StorageAccountType')]"
      }
    },
    {
      "type": "Microsoft.Network/publicIPAddresses",
      "name": "[concat(variables('PublicIPName'), copyindex())]",
      "copy": {
        "name": "ipLoop",
        "count": "[parameters('NumInstances')]"
      },
      "location": "[parameters('Location')]",
      "apiVersion": "2015-05-01-preview",
      "properties": {
        "publicIPAllocationMethod": "[variables('PublicIPAllocationMethod')]"
      }
    },
    {
      "type": "Microsoft.Network/networkSecurityGroups",
      "name": "[variables('NetworkSecurityGroupName')]",
      "location": "[parameters('Location')]",
      "apiVersion": "2015-05-01-preview",
      "properties": {
        "securityRules": [
          {
            "name": "HTTP",
            "properties": {
              "description": "Allows HTTP traffic",
              "protocol": "Tcp",
              "sourcePortRange": "*",
              "destinationPortRange": "80",
              "sourceAddressPrefix": "*",
              "destinationAddressPrefix": "*",
              "access": "Allow",
              "priority": 100,
              "direction": "Inbound"
            }
          },
          {
            "name": "HTTPS",
            "properties": {
              "description": "Allows HTTPS traffic",
              "protocol": "Tcp",
              "sourcePortRange": "*",
              "destinationPortRange": "443",
              "sourceAddressPrefix": "*",
              "destinationAddressPrefix": "*",
              "access": "Allow",
              "priority": 102,
              "direction": "Inbound"
            }
          },
          {
            "name": "SSH",
            "properties": {
              "description": "Allows SSH traffic",
              "protocol": "Tcp",
              "sourcePortRange": "*",
              "destinationPortRange": "22",
              "sourceAddressPrefix": "*",
              "destinationAddressPrefix": "*",
              "access": "Allow",
              "priority": 103,
              "direction": "Inbound"
            }
          },
          {
            "name": "OtherTCP",
            "properties": {
              "description": "Allows user-defined TCP services",
              "protocol": "Tcp",
              "sourcePortRange": "*",
              "destinationPortRange": "3000-3500",
              "sourceAddressPrefix": "*",
              "destinationAddressPrefix": "*",
              "access": "Allow",
              "priority": 105,
              "direction": "Inbound"
            }
          }
        ]
      }
    },
    {
      "type": "Microsoft.Network/virtualNetworks",
      "name": "[variables('VirtualNetworkName')]",
      "location": "[parameters('Location')]",
      "apiVersion": "2015-05-01-preview",
      "dependsOn": [
        "[concat('Microsoft.Network/networkSecurityGroups/', variables('NetworkSecurityGroupName'))]"
      ],
      "properties": {
        "addressSpace": {
          "addressPrefixes": [
              "[variables('AddressPrefix')]"
          ]
        },
        "subnets": [
          {
            "name": "[variables('SubnetName')]",
            "properties": {
              "addressPrefix": "[variables('SubnetPrefix')]",
              "networkSecurityGroup": {
                "id": "[resourceId('Microsoft.Network/networkSecurityGroups', variables('NetworkSecurityGroupName'))]"
              }
            }
          }
        ]
      }
    },
    {
      "type": "Microsoft.Network/networkInterfaces",
      "name": "[concat(variables('NetworkInterfaceName'), copyindex())]",
      "copy": {
        "name": "nicLoop",
        "count": "[parameters('NumInstances')]"
      },
      "location": "[parameters('Location')]",
      "dependsOn": [
        "[concat('Microsoft.Network/publicIPAddresses/', variables('PublicIPName'), copyindex())]",
        "[concat('Microsoft.Network/virtualNetworks/', variables('VirtualNetworkName'))]"
      ],
      "apiVersion": "2015-05-01-preview",
      "properties": {
        "ipConfigurations": [
          {
              "name": "ipconfig",
              "properties": {
                  "privateIPAllocationMethod": "[variables('PrivateIPAllocationMethod')]",
                  "publicIPAddress": {
                      "id": "[resourceId('Microsoft.Network/publicIpAddresses', concat(variables('PublicIPName'), copyindex()))]"
                  },
                  "subnet": {
                      "id": "[variables('SubnetRef')]"
                  }
              }
          }
        ]
      }
    },
    {
      "type": "Microsoft.Compute/virtualMachines",
      "name": "[concat(variables('VirtualMachineName'), copyindex())]",
      "copy": {
        "name": "vmLoop",
        "count": "[parameters('NumInstances')]"
      },
      "location": "[parameters('Location')]",
      "dependsOn": [
        "[concat('Microsoft.Storage/storageAccounts/', variables('StorageAccountName'))]",
        "[concat('Microsoft.Network/networkInterfaces/', variables('NetworkInterfaceName'), copyindex())]"
      ],
      "apiVersion": "2015-05-01-preview",
      "properties": {
        "hardwareProfile": {
          "vmSize": "[parameters('VirtualMachineSize')]"
        },
        "osProfile": {
          "computername": "[concat(variables('VirtualMachineName'), copyindex())]",
          "adminUsername": "[parameters('VirtualMachineUser')]",
          "linuxConfiguration": {
            "disablePasswordAuthentication": "true",
            "ssh": {
              "publicKeys": [
                {
                  "path": "[variables('AuthorizedKeysPath')]",
                  "keyData": "[parameters('SSHPublicKey')]"
                }
              ]
            }
          }
        },
        "storageProfile": {
          "imageReference": {
            "publisher": "Canonical",
            "offer": "UbuntuServer",
            "sku": "16.04.0-LTS",
            "version": "latest"
          },
          "osDisk": {
            "name": "[concat(variables('VirtualMachineName'), copyindex())]",
            "vhd": {
              "uri": "[concat('http://', variables('StorageAccountName'),'.blob.core.windows.net/vhds/', variables('VirtualMachineName'), copyindex(),'.vhd')]"
            },
            "caching": "ReadWrite",
            "createOption": "FromImage"
          }
        },
        "networkProfile": {
          "networkInterfaces": [
            {
              "id": "[resourceId('Microsoft.Network/networkInterfaces', concat(variables('NetworkInterfaceName'), copyindex()))]"
            }
          ]
        }
      }
    }
  ],
  "outputs": {
    "publicIPAddresses": {
      "type": "array",
      "value": [
        {{$instances := .Instances}}
        {{range $i, $_ := $instances}}
        "[resourceId('Microsoft.Network/publicIPAddresses', concat(variables('PublicIPName'), '{{$i}}'))]"
        {{if notLast $instances $i}},{{end}}
        {{end}}
      ]
    }
  }
}
`))
