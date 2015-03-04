package installer

import "text/template"

var stackTemplate = template.Must(template.New("stack_template.json").Parse(`
{
  "AWSTemplateFormatVersion": "2010-09-09",

  "Parameters": {
    "ClusterDomain": {
      "Type": "String",
      "Description": "Domain name to use for cluster"
    },
    "ImageId": {
      "Type": "String",
      "Description": "AMI to launch instance with"
    },
    "KeyName": {
      "Type": "String",
      "Description": "Name of EC2 Key pair"
    },
    "InstanceType": {
      "Type": "String",
      "ConstraintDescription": "Must be a valid EC2 instance type.",
      "Default": "{{.DefaultInstanceType}}",
      "Description": "EC2 instance type"
    },
    "VolumeSize": {
      "Type": "String",
      "Description": "Size of instance volumes in GB",
      "Default": "50"
    },
    "UserData": {
      "Type": "String",
      "Description": "The user data each instance is started with."
    },
    "VpcCidrBlock": {
      "Type": "String",
      "Description": "The CIDR block to use for the VPC",
      "Default": "10.0.0.0/16"
    },
    "SubnetCidrBlock": {
      "Type": "String",
      "Description": "The CIDR block to use for the subnet",
      "Default": "10.0.0.0/21"
    }
  },

  "Resources": {
    "VPC": {
      "Type": "AWS::EC2::VPC",
      "Properties": {
        "CidrBlock": { "Ref": "VpcCidrBlock" }
      }
    },

    "Gateway": {
      "Type": "AWS::EC2::InternetGateway"
    },

    "GatewayAttachment": {
      "Type": "AWS::EC2::VPCGatewayAttachment",
      "Properties": {
        "InternetGatewayId": { "Ref": "Gateway" },
        "VpcId": { "Ref": "VPC" }
      }
    },

    "GatewayRouteTable": {
      "Type": "AWS::EC2::RouteTable",
      "Properties": {
        "VpcId": { "Ref": "VPC" }
      }
    },

    "GatewayRoute": {
      "Type": "AWS::EC2::Route",
      "DependsOn": "GatewayAttachment",
      "Properties": {
        "DestinationCidrBlock": "0.0.0.0/0",
        "GatewayId": { "Ref": "Gateway" },
        "RouteTableId": { "Ref": "GatewayRouteTable" }
      }
    },

    "Subnet": {
      "Type": "AWS::EC2::Subnet",
      "Properties": {
        "CidrBlock": { "Ref": "SubnetCidrBlock" },
        "VpcId": { "Ref": "VPC" }
      }
    },

    "SubnetRoute": {
      "Type": "AWS::EC2::SubnetRouteTableAssociation",
      "Properties": {
        "RouteTableId": { "Ref": "GatewayRouteTable" },
        "SubnetId": { "Ref": "Subnet" }
      }
    },

    "PublicSecurityGroup": {
      "Type": "AWS::EC2::SecurityGroup",
      "Properties": {
        "GroupDescription": "flynn public ports",
        "VpcId": { "Ref": "VPC" },
        "SecurityGroupIngress": [
          {
            "IpProtocol": "tcp",
            "FromPort": "2222",
            "ToPort": "2222",
            "CidrIp": "0.0.0.0/0"
          },
          {
            "IpProtocol": "tcp",
            "FromPort": "22",
            "ToPort": "22",
            "CidrIp": "0.0.0.0/0"
          },
          {
            "IpProtocol": "tcp",
            "FromPort": "80",
            "ToPort": "80",
            "CidrIp": "0.0.0.0/0"
          },
          {
            "IpProtocol": "tcp",
            "FromPort": "443",
            "ToPort": "443",
            "CidrIp": "0.0.0.0/0"
          },
          {
            "IpProtocol": "tcp",
            "FromPort": "3000",
            "ToPort": "3500",
            "CidrIp": "0.0.0.0/0"
          },
          {
            "IpProtocol": "icmp",
            "FromPort": "0",
            "ToPort": "-1",
            "CidrIp": "0.0.0.0/0"
          },
          {
            "IpProtocol": "icmp",
            "FromPort": "3",
            "ToPort": "-1",
            "CidrIp": "0.0.0.0/0"
          }
        ]
      }
    },

    {{range $i, $_ := .Instances}}

    "Instance{{$i}}": {
      "Type": "AWS::EC2::Instance",
      "Properties": {
        "ImageId": { "Ref": "ImageId" },
        "InstanceType": { "Ref": "InstanceType" },
        "AvailabilityZone": { "Fn::GetAtt": ["Subnet", "AvailabilityZone"] },
        "KeyName": { "Ref": "KeyName" },
        "BlockDeviceMappings": [
          {
            "DeviceName": "/dev/sda1",
            "Ebs": {
              "VolumeSize": { "Ref" : "VolumeSize" },
              "VolumeType": "gp2"
            }
          }
        ],
        "NetworkInterfaces": [
          {
            "DeviceIndex": 0,
            "AssociatePublicIpAddress": true,
            "SubnetId": { "Ref": "Subnet" },
            "GroupSet": [
              { "Ref": "PublicSecurityGroup" },
              { "Fn::GetAtt": ["VPC", "DefaultSecurityGroup"] }
            ]
          }
        ],
        "UserData": { "Ref": "UserData" }
      }
    },

    "Instance{{$i}}HealthCheck": {
      "Type": "AWS::Route53::HealthCheck",
      "Properties": {
        "HealthCheckConfig": {
          "Type": "HTTP",
          "IPAddress": { "Fn::GetAtt": ["Instance{{$i}}", "PublicIp"] },
          "ResourcePath": "/ping"
        }
      }
    },

    "DNSRecords": {
      "Type": "AWS::Route53::RecordSetGroup",
      "Properties": {
        "HostedZoneId": { "Ref": "DNSZone" },
        "RecordSets": [
          {
            "Name": { "Fn::Join": [".", [{ "Ref": "ClusterDomain" }, ""]] },
            "SetIdentifier": "frontend0",
            "HealthCheckId": { "Ref": "Instance{{$i}}HealthCheck" },
            "Weight": 10,
            "Type": "A",
            "ResourceRecords": [
              { "Fn::GetAtt": ["Instance{{$i}}", "PublicIp"] }
            ],
            "TTL": "60"
          },
          {
            "Name": { "Fn::Join": [".", ["*", { "Ref": "ClusterDomain" }, ""]] },
            "Type": "CNAME",
            "ResourceRecords": [
              { "Fn::Join": [".", [{ "Ref": "ClusterDomain" }, ""]] }
            ],
            "TTL": "3600"
          }
        ]
      }
    },

    {{end}}

    "DNSZone": {
      "Type": "AWS::Route53::HostedZone",
      "Properties": {
        "Name": { "Ref": "ClusterDomain" }
      }
    }
  },

  "Outputs": {
    {{range $i, $_ := .Instances}}
      "IPAddress{{$i}}": {
        "Value": { "Fn::GetAtt": ["Instance{{$i}}", "PublicIp"] }
      },
    {{end}}
    "DNSZoneID": {
      "Value": { "Ref": "DNSZone" }
    }
  }
}
`))
