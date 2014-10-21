package ec2_test

var ErrorDump = `
<?xml version="1.0" encoding="UTF-8"?>
<Response><Errors><Error><Code>UnsupportedOperation</Code>
<Message>AMIs with an instance-store root device are not supported for the instance type 't1.micro'.</Message>
</Error></Errors><RequestID>0503f4e9-bbd6-483c-b54f-c4ae9f3b30f4</RequestID></Response>
`

// http://goo.gl/Mcm3b
var RunInstancesExample = `
<RunInstancesResponse xmlns="http://ec2.amazonaws.com/doc/2013-10-13/"> 
  <requestId>59dbff89-35bd-4eac-99ed-be587EXAMPLE</requestId> 
  <reservationId>r-47a5402e</reservationId> 
  <ownerId>999988887777</ownerId>
  <groupSet>
      <item>
          <groupId>sg-67ad940e</groupId>
          <groupName>default</groupName>
      </item>
  </groupSet>
  <instancesSet>
    <item>
      <instanceId>i-2ba64342</instanceId>
      <imageId>ami-60a54009</imageId>
      <instanceState>
        <code>0</code>
        <name>pending</name>
      </instanceState>
      <privateDnsName></privateDnsName>
      <dnsName></dnsName>
      <keyName>example-key-name</keyName>
      <amiLaunchIndex>0</amiLaunchIndex>
      <instanceType>m1.small</instanceType>
      <launchTime>2007-08-07T11:51:50.000Z</launchTime>
      <placement>
        <availabilityZone>us-east-1b</availabilityZone>
      </placement>
      <monitoring>
        <state>enabled</state>
      </monitoring>
      <virtualizationType>paravirtual</virtualizationType>
      <subnetId>subnet-id</subnetId>
      <vpcId>vpc-id</vpcId>
      <sourceDestCheck>true</sourceDestCheck>
      <clientToken/>
      <tagSet/>
      <hypervisor>xen</hypervisor>
      <networkInterfaceSet>
        <item>
          <networkInterfaceId>eni-c6bb50ae</networkInterfaceId>
          <subnetId>subnet-id</subnetId>
          <vpcId>vpc-id</vpcId>
          <description>eth0</description>
          <ownerId>111122223333</ownerId>
          <status>in-use</status>
          <privateIpAddress>10.0.0.25</privateIpAddress>
          <macAddress>11:22:33:44:55:66</macAddress>
          <sourceDestCheck>true</sourceDestCheck>
          <groupSet>
            <item>
              <groupId>sg-1</groupId>
              <groupName>vpc sg-1</groupName>
            </item>
            <item>
              <groupId>sg-2</groupId>
              <groupName>vpc sg-2</groupName>
            </item>
          </groupSet>
          <attachment>
            <attachmentId>eni-attach-0326646a</attachmentId>
            <deviceIndex>0</deviceIndex>
            <status>attaching</status>
            <attachTime>2011-12-20T08:29:31.000Z</attachTime>
            <deleteOnTermination>true</deleteOnTermination>
          </attachment>
          <privateIpAddressesSet>
            <item>
              <privateIpAddress>10.0.0.25</privateIpAddress>
              <primary>true</primary>
            </item>
          </privateIpAddressesSet>
        </item>
        <item>
          <networkInterfaceId>eni-id</networkInterfaceId>
          <subnetId>subnet-id</subnetId>
          <vpcId>vpc-id</vpcId>
          <description/>
          <ownerId>111122223333</ownerId>
          <status>in-use</status>
          <privateIpAddress>10.0.1.10</privateIpAddress>
          <macAddress>11:22:33:44:55:66</macAddress>
          <sourceDestCheck>true</sourceDestCheck>
          <groupSet>
            <item>
              <groupId>sg-id</groupId>
              <groupName>vpc default</groupName>
            </item>
          </groupSet>
          <attachment>
            <attachmentId>eni-attach-id</attachmentId>
            <deviceIndex>1</deviceIndex>
            <status>attaching</status>
            <attachTime>2011-12-20T08:29:31.000Z</attachTime>
            <deleteOnTermination>false</deleteOnTermination>
          </attachment>
          <privateIpAddressesSet>
            <item>
              <privateIpAddress>10.0.1.10</privateIpAddress>
              <primary>true</primary>
            </item>
            <item>
              <privateIpAddress>10.0.1.20</privateIpAddress>
              <primary>false</primary>
            </item>
          </privateIpAddressesSet>
        </item>
      </networkInterfaceSet>
    </item>
    <item>
      <instanceId>i-2bc64242</instanceId>
      <imageId>ami-60a54009</imageId>
      <instanceState>
        <code>0</code>
        <name>pending</name>
      </instanceState>
      <privateDnsName></privateDnsName>
      <dnsName></dnsName>
      <keyName>example-key-name</keyName>
      <amiLaunchIndex>1</amiLaunchIndex>
      <instanceType>m1.small</instanceType>
      <launchTime>2007-08-07T11:51:50.000Z</launchTime>
      <placement>
         <availabilityZone>us-east-1b</availabilityZone>
      </placement>
      <monitoring>
        <state>enabled</state>
      </monitoring>
      <virtualizationType>paravirtual</virtualizationType>
      <clientToken/>
      <tagSet/>
      <hypervisor>xen</hypervisor>
    </item>
    <item>
      <instanceId>i-2be64332</instanceId>
      <imageId>ami-60a54009</imageId>
      <instanceState>
        <code>0</code>
        <name>pending</name>
      </instanceState>
      <privateDnsName></privateDnsName>
      <dnsName></dnsName>
      <keyName>example-key-name</keyName>
      <amiLaunchIndex>2</amiLaunchIndex>
      <instanceType>m1.small</instanceType>
      <launchTime>2007-08-07T11:51:50.000Z</launchTime>
      <placement>
         <availabilityZone>us-east-1b</availabilityZone>
      </placement>
      <monitoring>
        <state>enabled</state>
      </monitoring>
      <virtualizationType>paravirtual</virtualizationType>
      <clientToken/>
      <tagSet/>
      <hypervisor>xen</hypervisor>
    </item>
  </instancesSet>
</RunInstancesResponse>
`

// http://goo.gl/3BKHj
var TerminateInstancesExample = `
<TerminateInstancesResponse xmlns="http://ec2.amazonaws.com/doc/2011-12-15/">
  <requestId>59dbff89-35bd-4eac-99ed-be587EXAMPLE</requestId> 
  <instancesSet>
    <item>
      <instanceId>i-3ea74257</instanceId>
      <currentState>
        <code>32</code>
        <name>shutting-down</name>
      </currentState>
      <previousState>
        <code>16</code>
        <name>running</name>
      </previousState>
    </item>
  </instancesSet>
</TerminateInstancesResponse>
`

// http://goo.gl/mLbmw
var DescribeInstancesExample1 = `
<DescribeInstancesResponse xmlns="http://ec2.amazonaws.com/doc/2011-12-15/">
  <requestId>98e3c9a4-848c-4d6d-8e8a-b1bdEXAMPLE</requestId>
  <reservationSet>
    <item>
      <reservationId>r-b27e30d9</reservationId>
      <ownerId>999988887777</ownerId>
      <groupSet>
        <item>
          <groupId>sg-67ad940e</groupId>
          <groupName>default</groupName>
        </item>
      </groupSet>
      <instancesSet>
        <item>
          <instanceId>i-c5cd56af</instanceId>
          <imageId>ami-1a2b3c4d</imageId>
          <instanceState>
            <code>16</code>
            <name>running</name>
          </instanceState>
          <privateDnsName>domU-12-31-39-10-56-34.compute-1.internal</privateDnsName>
          <dnsName>ec2-174-129-165-232.compute-1.amazonaws.com</dnsName>
          <reason/>
          <keyName>GSG_Keypair</keyName>
          <amiLaunchIndex>0</amiLaunchIndex>
          <productCodes/>
          <instanceType>m1.small</instanceType>
          <launchTime>2010-08-17T01:15:18.000Z</launchTime>
          <placement>
            <availabilityZone>us-east-1b</availabilityZone>
            <groupName/>
          </placement>
          <kernelId>aki-94c527fd</kernelId>
          <ramdiskId>ari-96c527ff</ramdiskId>
          <monitoring>
            <state>disabled</state>
          </monitoring>
          <privateIpAddress>10.198.85.190</privateIpAddress>
          <ipAddress>174.129.165.232</ipAddress>
          <architecture>i386</architecture>
          <rootDeviceType>ebs</rootDeviceType>
          <rootDeviceName>/dev/sda1</rootDeviceName>
          <blockDeviceMapping>
            <item>
              <deviceName>/dev/sda1</deviceName>
              <ebs>
                <volumeId>vol-a082c1c9</volumeId>
                <status>attached</status>
                <attachTime>2010-08-17T01:15:21.000Z</attachTime>
                <deleteOnTermination>false</deleteOnTermination>
              </ebs>
            </item>
          </blockDeviceMapping>
          <instanceLifecycle>spot</instanceLifecycle>
          <spotInstanceRequestId>sir-7a688402</spotInstanceRequestId>
          <virtualizationType>paravirtual</virtualizationType>
          <clientToken/>
          <tagSet/>
          <hypervisor>xen</hypervisor>
          <groupSet>
            <item>
              <groupId>sg-67ad940e</groupId>
              <groupName>default</groupName>
            </item>
          </groupSet>
       </item>
      </instancesSet>
      <requesterId>854251627541</requesterId>
    </item>
    <item>
      <reservationId>r-b67e30dd</reservationId>
      <ownerId>999988887777</ownerId>
      <groupSet>
        <item>
          <groupId>sg-67ad940e</groupId>
          <groupName>default</groupName>
        </item>
      </groupSet>
      <instancesSet>
        <item>
          <instanceId>i-d9cd56b3</instanceId>
          <imageId>ami-1a2b3c4d</imageId>
          <instanceState>
            <code>16</code>
            <name>running</name>
          </instanceState>
          <privateDnsName>domU-12-31-39-10-54-E5.compute-1.internal</privateDnsName>
          <dnsName>ec2-184-73-58-78.compute-1.amazonaws.com</dnsName>
          <reason/>
          <keyName>GSG_Keypair</keyName>
          <amiLaunchIndex>0</amiLaunchIndex>
          <productCodes/>
          <instanceType>m1.large</instanceType>
          <launchTime>2010-08-17T01:15:19.000Z</launchTime>
          <placement>
            <availabilityZone>us-east-1b</availabilityZone>
            <groupName/>
          </placement>
          <kernelId>aki-94c527fd</kernelId>
          <ramdiskId>ari-96c527ff</ramdiskId>
          <monitoring>
            <state>disabled</state>
          </monitoring>
          <privateIpAddress>10.198.87.19</privateIpAddress>
          <ipAddress>184.73.58.78</ipAddress>
          <architecture>i386</architecture>
          <rootDeviceType>ebs</rootDeviceType>
          <rootDeviceName>/dev/sda1</rootDeviceName>
          <blockDeviceMapping>
            <item>
              <deviceName>/dev/sda1</deviceName>
              <ebs>
                <volumeId>vol-a282c1cb</volumeId>
                <status>attached</status>
                <attachTime>2010-08-17T01:15:23.000Z</attachTime>
                <deleteOnTermination>false</deleteOnTermination>
              </ebs>
            </item>
          </blockDeviceMapping>
          <instanceLifecycle>spot</instanceLifecycle>
          <spotInstanceRequestId>sir-55a3aa02</spotInstanceRequestId>
          <virtualizationType>paravirtual</virtualizationType>
          <clientToken/>
          <tagSet/>
          <hypervisor>xen</hypervisor>
       </item>
      </instancesSet>
      <requesterId>854251627541</requesterId>
    </item>
  </reservationSet>
</DescribeInstancesResponse>
`

// http://goo.gl/mLbmw
var DescribeInstancesExample2 = `
<DescribeInstancesResponse xmlns="http://ec2.amazonaws.com/doc/2011-12-15/"> 
  <requestId>59dbff89-35bd-4eac-99ed-be587EXAMPLE</requestId> 
  <reservationSet>
    <item>
      <reservationId>r-bc7e30d7</reservationId>
      <ownerId>999988887777</ownerId>
      <groupSet>
        <item>
          <groupId>sg-67ad940e</groupId>
          <groupName>default</groupName>
        </item>
      </groupSet>
      <instancesSet>
        <item>
          <instanceId>i-c7cd56ad</instanceId>
          <imageId>ami-b232d0db</imageId>
          <instanceState>
            <code>16</code>
            <name>running</name>
          </instanceState>
          <privateDnsName>domU-12-31-39-01-76-06.compute-1.internal</privateDnsName>
          <dnsName>ec2-72-44-52-124.compute-1.amazonaws.com</dnsName>
          <keyName>GSG_Keypair</keyName>
          <amiLaunchIndex>0</amiLaunchIndex>
          <productCodes/>
          <instanceType>m1.small</instanceType>
          <launchTime>2010-08-17T01:15:16.000Z</launchTime>
          <placement>
              <availabilityZone>us-east-1b</availabilityZone>
          </placement>
          <kernelId>aki-94c527fd</kernelId>
          <ramdiskId>ari-96c527ff</ramdiskId>
          <monitoring>
              <state>disabled</state>
          </monitoring>
          <privateIpAddress>10.255.121.240</privateIpAddress>
          <ipAddress>72.44.52.124</ipAddress>
          <architecture>i386</architecture>
          <rootDeviceType>ebs</rootDeviceType>
          <rootDeviceName>/dev/sda1</rootDeviceName>
          <blockDeviceMapping>
              <item>
                 <deviceName>/dev/sda1</deviceName>
                 <ebs>
                    <volumeId>vol-a482c1cd</volumeId>
                    <status>attached</status>
                    <attachTime>2010-08-17T01:15:26.000Z</attachTime>
                    <deleteOnTermination>true</deleteOnTermination>
                </ebs>
             </item>
          </blockDeviceMapping>
          <virtualizationType>paravirtual</virtualizationType>
          <clientToken/>
          <tagSet>
              <item>
                    <key>webserver</key>
                    <value></value>
             </item>
              <item>
                    <key>stack</key>
                    <value>Production</value>
             </item>
          </tagSet>
          <hypervisor>xen</hypervisor>
        </item>
      </instancesSet>
    </item>
  </reservationSet>
</DescribeInstancesResponse>
`

// http://goo.gl/V0U25
var DescribeImagesExample = `
<DescribeImagesResponse xmlns="http://ec2.amazonaws.com/doc/2012-08-15/">
         <requestId>4a4a27a2-2e7c-475d-b35b-ca822EXAMPLE</requestId>
    <imagesSet>
        <item>
            <imageId>ami-a2469acf</imageId>
            <imageLocation>aws-marketplace/example-marketplace-amzn-ami.1</imageLocation>
            <imageState>available</imageState>
            <imageOwnerId>123456789999</imageOwnerId>
            <isPublic>true</isPublic>
            <productCodes>
                <item>
                    <productCode>a1b2c3d4e5f6g7h8i9j10k11</productCode>
                    <type>marketplace</type>
                </item>
            </productCodes>
            <architecture>i386</architecture>
            <imageType>machine</imageType>
            <kernelId>aki-805ea7e9</kernelId>
            <imageOwnerAlias>aws-marketplace</imageOwnerAlias>
            <name>example-marketplace-amzn-ami.1</name>
            <description>Amazon Linux AMI i386 EBS</description>
            <rootDeviceType>ebs</rootDeviceType>
            <rootDeviceName>/dev/sda1</rootDeviceName>
            <blockDeviceMapping>
                <item>
                    <deviceName>/dev/sda1</deviceName>
                    <ebs>
                        <snapshotId>snap-787e9403</snapshotId>
                        <volumeSize>8</volumeSize>
                        <deleteOnTermination>true</deleteOnTermination>
                    </ebs>
                </item>
            </blockDeviceMapping>
            <virtualizationType>paravirtual</virtualizationType>
            <hypervisor>xen</hypervisor>
        </item>
    </imagesSet>
</DescribeImagesResponse>
`

// http://goo.gl/ttcda
var CreateSnapshotExample = `
<CreateSnapshotResponse xmlns="http://ec2.amazonaws.com/doc/2012-10-01/">
  <requestId>59dbff89-35bd-4eac-99ed-be587EXAMPLE</requestId>
  <snapshotId>snap-78a54011</snapshotId>
  <volumeId>vol-4d826724</volumeId>
  <status>pending</status>
  <startTime>2008-05-07T12:51:50.000Z</startTime>
  <progress>60%</progress>
  <ownerId>111122223333</ownerId>
  <volumeSize>10</volumeSize>
  <description>Daily Backup</description>
</CreateSnapshotResponse>
`

// http://goo.gl/vwU1y
var DeleteSnapshotExample = `
<DeleteSnapshotResponse xmlns="http://ec2.amazonaws.com/doc/2012-10-01/">
  <requestId>59dbff89-35bd-4eac-99ed-be587EXAMPLE</requestId> 
  <return>true</return>
</DeleteSnapshotResponse>
`

// http://goo.gl/nkovs
var DescribeSnapshotsExample = `
<DescribeSnapshotsResponse xmlns="http://ec2.amazonaws.com/doc/2012-10-01/">
   <requestId>59dbff89-35bd-4eac-99ed-be587EXAMPLE</requestId> 
   <snapshotSet>
      <item>
         <snapshotId>snap-1a2b3c4d</snapshotId>
         <volumeId>vol-8875daef</volumeId>
         <status>pending</status>
         <startTime>2010-07-29T04:12:01.000Z</startTime>
         <progress>30%</progress>
         <ownerId>111122223333</ownerId>
         <volumeSize>15</volumeSize>
         <description>Daily Backup</description>
         <tagSet>
            <item>
               <key>Purpose</key>
               <value>demo_db_14_backup</value>
            </item>
         </tagSet>
      </item>
   </snapshotSet>
</DescribeSnapshotsResponse>
`

// http://goo.gl/Eo7Yl
var CreateSecurityGroupExample = `
<CreateSecurityGroupResponse xmlns="http://ec2.amazonaws.com/doc/2011-12-15/">
   <requestId>59dbff89-35bd-4eac-99ed-be587EXAMPLE</requestId>
   <return>true</return>
   <groupId>sg-67ad940e</groupId>
</CreateSecurityGroupResponse>
`

// http://goo.gl/k12Uy
var DescribeSecurityGroupsExample = `
<DescribeSecurityGroupsResponse xmlns="http://ec2.amazonaws.com/doc/2011-12-15/">
  <requestId>59dbff89-35bd-4eac-99ed-be587EXAMPLE</requestId> 
  <securityGroupInfo>
    <item>
      <ownerId>999988887777</ownerId>
      <groupName>WebServers</groupName>
      <groupId>sg-67ad940e</groupId>
      <groupDescription>Web Servers</groupDescription>
      <ipPermissions>
        <item>
           <ipProtocol>tcp</ipProtocol>
           <fromPort>80</fromPort>
           <toPort>80</toPort>
           <groups/>
           <ipRanges>
             <item>
               <cidrIp>0.0.0.0/0</cidrIp>
             </item>
           </ipRanges>
        </item>
      </ipPermissions>
    </item>
    <item>
      <ownerId>999988887777</ownerId>
      <groupName>RangedPortsBySource</groupName>
      <groupId>sg-76abc467</groupId>
      <groupDescription>Group A</groupDescription>
      <ipPermissions>
        <item>
           <ipProtocol>tcp</ipProtocol>
           <fromPort>6000</fromPort>
           <toPort>7000</toPort>
           <groups/>
           <ipRanges/>
        </item>
      </ipPermissions>
    </item>
  </securityGroupInfo>
</DescribeSecurityGroupsResponse>
`

// A dump which includes groups within ip permissions.
var DescribeSecurityGroupsDump = `
<?xml version="1.0" encoding="UTF-8"?>
<DescribeSecurityGroupsResponse xmlns="http://ec2.amazonaws.com/doc/2011-12-15/">
    <requestId>87b92b57-cc6e-48b2-943f-f6f0e5c9f46c</requestId>
    <securityGroupInfo>
        <item>
            <ownerId>12345</ownerId>
            <groupName>default</groupName>
            <groupDescription>default group</groupDescription>
            <ipPermissions>
                <item>
                    <ipProtocol>icmp</ipProtocol>
                    <fromPort>-1</fromPort>
                    <toPort>-1</toPort>
                    <groups>
                        <item>
                            <userId>12345</userId>
                            <groupName>default</groupName>
                            <groupId>sg-67ad940e</groupId>
                        </item>
                    </groups>
                    <ipRanges/>
                </item>
                <item>
                    <ipProtocol>tcp</ipProtocol>
                    <fromPort>0</fromPort>
                    <toPort>65535</toPort>
                    <groups>
                        <item>
                            <userId>12345</userId>
                            <groupName>other</groupName>
                            <groupId>sg-76abc467</groupId>
                        </item>
                    </groups>
                    <ipRanges/>
                </item>
            </ipPermissions>
        </item>
    </securityGroupInfo>
</DescribeSecurityGroupsResponse>
`

// http://goo.gl/QJJDO
var DeleteSecurityGroupExample = `
<DeleteSecurityGroupResponse xmlns="http://ec2.amazonaws.com/doc/2011-12-15/">
   <requestId>59dbff89-35bd-4eac-99ed-be587EXAMPLE</requestId>
   <return>true</return>
</DeleteSecurityGroupResponse>
`

// http://goo.gl/u2sDJ
var AuthorizeSecurityGroupIngressExample = `
<AuthorizeSecurityGroupIngressResponse xmlns="http://ec2.amazonaws.com/doc/2011-12-15/">
  <requestId>59dbff89-35bd-4eac-99ed-be587EXAMPLE</requestId>
  <return>true</return>
</AuthorizeSecurityGroupIngressResponse>
`

// http://goo.gl/Mz7xr
var RevokeSecurityGroupIngressExample = `
<RevokeSecurityGroupIngressResponse xmlns="http://ec2.amazonaws.com/doc/2011-12-15/">
  <requestId>59dbff89-35bd-4eac-99ed-be587EXAMPLE</requestId> 
  <return>true</return>
</RevokeSecurityGroupIngressResponse>
`

// http://goo.gl/Vmkqc
var CreateTagsExample = `
<CreateTagsResponse xmlns="http://ec2.amazonaws.com/doc/2011-12-15/">
   <requestId>59dbff89-35bd-4eac-99ed-be587EXAMPLE</requestId>
   <return>true</return>
</CreateTagsResponse>
`

// http://goo.gl/awKeF
var StartInstancesExample = `
<StartInstancesResponse xmlns="http://ec2.amazonaws.com/doc/2011-12-15/">
  <requestId>59dbff89-35bd-4eac-99ed-be587EXAMPLE</requestId>   
  <instancesSet>
    <item>
      <instanceId>i-10a64379</instanceId>
      <currentState>
          <code>0</code>
          <name>pending</name>
      </currentState>
      <previousState>
          <code>80</code>
          <name>stopped</name>
      </previousState>
    </item>
  </instancesSet>
</StartInstancesResponse>
`

// http://goo.gl/436dJ
var StopInstancesExample = `
<StopInstancesResponse xmlns="http://ec2.amazonaws.com/doc/2011-12-15/">
  <requestId>59dbff89-35bd-4eac-99ed-be587EXAMPLE</requestId> 
  <instancesSet>
    <item>
      <instanceId>i-10a64379</instanceId>
      <currentState>
          <code>64</code>
          <name>stopping</name>
      </currentState>
      <previousState>
          <code>16</code>
          <name>running</name>
      </previousState>
    </item>
  </instancesSet>
</StopInstancesResponse>
`

// http://goo.gl/baoUf
var RebootInstancesExample = `
<RebootInstancesResponse xmlns="http://ec2.amazonaws.com/doc/2011-12-15/">
  <requestId>59dbff89-35bd-4eac-99ed-be587EXAMPLE</requestId>
  <return>true</return>
</RebootInstancesResponse>
`

// http://goo.gl/nkwjv
var CreateVpcExample = `
<CreateVpcResponse xmlns="http://ec2.amazonaws.com/doc/2013-10-15/">
   <requestId>7a62c49f-347e-4fc4-9331-6e8eEXAMPLE</requestId>
   <vpc>
      <vpcId>vpc-1a2b3c4d</vpcId>
      <state>pending</state>
      <cidrBlock>10.0.0.0/16</cidrBlock>
      <dhcpOptionsId>dopt-1a2b3c4d2</dhcpOptionsId>
      <instanceTenancy>default</instanceTenancy>
      <tagSet/>
   </vpc>
</CreateVpcResponse>
`

// http://goo.gl/bcxtbf
var DeleteVpcExample = `
<DeleteVpcResponse xmlns="http://ec2.amazonaws.com/doc/2013-10-15/">
   <requestId>7a62c49f-347e-4fc4-9331-6e8eEXAMPLE</requestId>
   <return>true</return>
</DeleteVpcResponse>
`

// http://goo.gl/Y5kHqG
var DescribeVpcsExample = `
<DescribeVpcsResponse xmlns="http://ec2.amazonaws.com/doc/2013-10-15/">
  <requestId>7a62c49f-347e-4fc4-9331-6e8eEXAMPLE</requestId>
  <vpcSet>
    <item>
      <vpcId>vpc-1a2b3c4d</vpcId>
      <state>available</state>
      <cidrBlock>10.0.0.0/23</cidrBlock>
      <dhcpOptionsId>dopt-7a8b9c2d</dhcpOptionsId>
      <instanceTenancy>default</instanceTenancy>
      <isDefault>false</isDefault>
      <tagSet/>
    </item>
  </vpcSet>
</DescribeVpcsResponse>
`

// http://goo.gl/wLPhf
var CreateSubnetExample = `
<CreateSubnetResponse xmlns="http://ec2.amazonaws.com/doc/2013-10-15/">
  <requestId>7a62c49f-347e-4fc4-9331-6e8eEXAMPLE</requestId>
  <subnet>
    <subnetId>subnet-9d4a7b6c</subnetId>
    <state>pending</state>
    <vpcId>vpc-1a2b3c4d</vpcId>
    <cidrBlock>10.0.1.0/24</cidrBlock>
    <availableIpAddressCount>251</availableIpAddressCount>
    <availabilityZone>us-east-1a</availabilityZone>
    <tagSet/>
  </subnet>
</CreateSubnetResponse>
`

// http://goo.gl/KmhcBM
var DeleteSubnetExample = `
<DeleteSubnetResponse xmlns="http://ec2.amazonaws.com/doc/2013-10-15/">
   <requestId>7a62c49f-347e-4fc4-9331-6e8eEXAMPLE</requestId>
   <return>true</return>
</DeleteSubnetResponse>
`

// http://goo.gl/NTKQVI
var DescribeSubnetsExample = `
<DescribeSubnetsResponse xmlns="http://ec2.amazonaws.com/doc/2013-10-15/">
  <requestId>7a62c49f-347e-4fc4-9331-6e8eEXAMPLE</requestId>
  <subnetSet>
    <item>
      <subnetId>subnet-9d4a7b6c</subnetId>
      <state>available</state>
      <vpcId>vpc-1a2b3c4d</vpcId>
      <cidrBlock>10.0.1.0/24</cidrBlock>
      <availableIpAddressCount>251</availableIpAddressCount>
      <availabilityZone>us-east-1a</availabilityZone>
      <defaultForAz>false</defaultForAz>
      <mapPublicIpOnLaunch>false</mapPublicIpOnLaunch>
      <tagSet/>
    </item>
    <item>
      <subnetId>subnet-6e7f829e</subnetId>
      <state>available</state>
      <vpcId>vpc-1a2b3c4d</vpcId>
      <cidrBlock>10.0.0.0/24</cidrBlock>
      <availableIpAddressCount>251</availableIpAddressCount>
      <availabilityZone>us-east-1a</availabilityZone>
      <defaultForAz>false</defaultForAz>
      <mapPublicIpOnLaunch>false</mapPublicIpOnLaunch>
      <tagSet/>
    </item>
  </subnetSet>
</DescribeSubnetsResponse>
`

// http://goo.gl/ze3VhA
var CreateNetworkInterfaceExample = `
<CreateNetworkInterfaceResponse xmlns="http://ec2.amazonaws.com/doc/2013-10-15/">
 <requestId>8dbe591e-5a22-48cb-b948-dd0aadd55adf</requestId>
    <networkInterface>
        <networkInterfaceId>eni-cfca76a6</networkInterfaceId>
        <subnetId>subnet-b2a249da</subnetId>
        <vpcId>vpc-c31dafaa</vpcId>
        <availabilityZone>ap-southeast-1b</availabilityZone>
        <description/>
        <ownerId>251839141158</ownerId>
        <requesterManaged>false</requesterManaged>
        <status>available</status>
        <macAddress>02:74:b0:72:79:61</macAddress>
        <privateIpAddress>10.0.2.157</privateIpAddress>
        <sourceDestCheck>true</sourceDestCheck>
        <groupSet>
            <item>
                <groupId>sg-1a2b3c4d</groupId>
                <groupName>default</groupName>
            </item>
        </groupSet>
        <tagSet/>
        <privateIpAddressesSet>
            <item>
                <privateIpAddress>10.0.2.157</privateIpAddress>
                <primary>true</primary>
            </item>
        </privateIpAddressesSet>
    </networkInterface>
</CreateNetworkInterfaceResponse>
`

// http://goo.gl/MC1yOj
var DeleteNetworkInterfaceExample = `
<DeleteNetworkInterfaceResponse xmlns="http://ec2.amazonaws.com/doc/2013-10-15/">
    <requestId>e1c6d73b-edaa-4e62-9909-6611404e1739</requestId>
    <return>true</return>
</DeleteNetworkInterfaceResponse>
`

// http://goo.gl/2LcXtM
var DescribeNetworkInterfacesExample = `
<DescribeNetworkInterfacesResponse xmlns="http://ec2.amazonaws.com/doc/2013-10-15/">
    <requestId>fc45294c-006b-457b-bab9-012f5b3b0e40</requestId>
     <networkInterfaceSet>
       <item>
         <networkInterfaceId>eni-0f62d866</networkInterfaceId>
         <subnetId>subnet-c53c87ac</subnetId>
         <vpcId>vpc-cc3c87a5</vpcId>
         <availabilityZone>ap-southeast-1b</availabilityZone>
         <description/>
         <ownerId>053230519467</ownerId>
         <requesterManaged>false</requesterManaged>
         <status>in-use</status>
         <macAddress>02:81:60:cb:27:37</macAddress>
         <privateIpAddress>10.0.0.146</privateIpAddress>
         <sourceDestCheck>true</sourceDestCheck>
         <groupSet>
           <item>
             <groupId>sg-3f4b5653</groupId>
             <groupName>default</groupName>
           </item>
         </groupSet>
         <attachment>
           <attachmentId>eni-attach-6537fc0c</attachmentId>
           <instanceId>i-22197876</instanceId>
           <instanceOwnerId>053230519467</instanceOwnerId>
           <deviceIndex>0</deviceIndex>
           <status>attached</status>
           <attachTime>2012-07-01T21:45:27.000Z</attachTime>
           <deleteOnTermination>true</deleteOnTermination>
         </attachment>
         <tagSet/>
         <privateIpAddressesSet>
           <item>
             <privateIpAddress>10.0.0.146</privateIpAddress>
             <primary>true</primary>
           </item>
           <item>
             <privateIpAddress>10.0.0.148</privateIpAddress>
             <primary>false</primary>
           </item>
           <item>
             <privateIpAddress>10.0.0.150</privateIpAddress>
             <primary>false</primary>
           </item>
         </privateIpAddressesSet>
       </item>
       <item>
         <networkInterfaceId>eni-a66ed5cf</networkInterfaceId>
         <subnetId>subnet-cd8a35a4</subnetId>
         <vpcId>vpc-f28a359b</vpcId>
         <availabilityZone>ap-southeast-1b</availabilityZone>
         <description>Primary network interface</description>
         <ownerId>053230519467</ownerId>
         <requesterManaged>false</requesterManaged>
         <status>in-use</status>
         <macAddress>02:78:d7:00:8a:1e</macAddress>
         <privateIpAddress>10.0.1.233</privateIpAddress>
         <sourceDestCheck>true</sourceDestCheck>
         <groupSet>
           <item>
             <groupId>sg-a2a0b2ce</groupId>
             <groupName>quick-start-1</groupName>
           </item>
         </groupSet>
         <attachment>
           <attachmentId>eni-attach-a99c57c0</attachmentId>
           <instanceId>i-886401dc</instanceId>
           <instanceOwnerId>053230519467</instanceOwnerId>
           <deviceIndex>0</deviceIndex>
           <status>attached</status>
           <attachTime>2012-06-27T20:08:44.000Z</attachTime>
           <deleteOnTermination>true</deleteOnTermination>
         </attachment>
         <tagSet/>
         <privateIpAddressesSet>
           <item>
             <privateIpAddress>10.0.1.233</privateIpAddress>
             <primary>true</primary>
           </item>
           <item>
             <privateIpAddress>10.0.1.20</privateIpAddress>
             <primary>false</primary>
           </item>
         </privateIpAddressesSet>
       </item>
     </networkInterfaceSet>
</DescribeNetworkInterfacesResponse>
`

// http://goo.gl/rEbSii
var AttachNetworkInterfaceExample = `
<AttachNetworkInterfaceResponse xmlns="http://ec2.amazonaws.com/doc/2013-10-15/">
    <requestId>ace8cd1e-e685-4e44-90fb-92014d907212</requestId>
    <attachmentId>eni-attach-d94b09b0</attachmentId>
</AttachNetworkInterfaceResponse>
`

// http://goo.gl/0Xc1px
var DetachNetworkInterfaceExample = `
<DetachNetworkInterfaceResponse xmlns="http://ec2.amazonaws.com/doc/2013-10-15/">
    <requestId>ce540707-0635-46bc-97da-33a8a362a0e8</requestId>
    <return>true</return>
</DetachNetworkInterfaceResponse>
`

// http://goo.gl/MoeH0L
var AssignPrivateIpAddressesExample = `
<AssignPrivateIpAddresses xmlns="http://ec2.amazonaws.com/doc/2013-10-15/">
   <requestId>59dbff89-35bd-4eac-99ed-be587EXAMPLE</requestId>
   <return>true</return>
</AssignPrivateIpAddresses>
`

// http://goo.gl/RjGZdB
var UnassignPrivateIpAddressesExample = `
<UnassignPrivateIpAddresses xmlns="http://ec2.amazonaws.com/doc/2013-10-15/">
   <requestId>59dbff89-35bd-4eac-99ed-be587EXAMPLE</requestId>
   <return>true</return>
</UnassignPrivateIpAddresses>
`
