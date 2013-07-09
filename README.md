## Flynn Spec

#### General Features

- Manages services in containers across a cluster
- HTTP RESTish API allows easy management of services/containers
- Comes with buildpacks and “git push” deployment
- Open source (BSD 3-clause) and runs on any infrastructure (AWS/Public Cloud, Private Cloud, Bare Metal)
- Designed with twelve-factor apps in mind
- Services are UNIX processes, totally language-agnostic
- Complex deployment options like staged rollouts, continuous deployment, etc. can be built by using the API
- Backing services (with persistent disk) may be deployed and managed using the same system
  - Provides HA and provisioning hooks
- HTTP and TCP routing/load-balancing provided
- The system itself is fully containerized, with few (if any) components not running in containers
- Systems utilizing containers (for example CI) can be built using the API


#### MVP Features

- API
  - CRUD /services
    - container management
      - scale up/down
      - push new image
      - stream logs
- Scheduler
- git push buildpack image builder
- HTTP router
- Command-line client


#### Final Product Features 

- TCP routing (with PROXY protocol)
- Disk-backed service support
  - Pin container to node for RAID/EBS usage
  - HA master/slave setup/failover hooks
  - DB provisioning hooks (create/destroy database)
- Run/attach (“heroku run”)
- User accounts with basic ACLs
- HA for routing, graceful failure for scheduler, etc.

#### Reach Goals

- AMI for easy deployment
- Prebuilt HA setups for common databases (Redis, Postgres, Riak, Mongo, MySQL)
- Monitoring system integration
- Graphite/metrics integration
- Service registry
- Professionally designed web dashboard
- More web dashboard stuff (databases, repo viewer, etc)
- Continuous integration tool
- Log Aggregation (compatible with open source analysis tools)
- ACLs/quotas (user, team, etc)
- Autoscaling
- HA control plane
- Scheduler auto-failover
- Priority (run Hadoop when there is cluster availability)
- Hadoop/Spark setups
- Ceph


#### Benefits

- Painless scaling, just add more nodes
- Painless deployment, just git push (“internal Heroku”)
- Ops provides PaaS as a product to software engineering, no messing around with configuration management scripts to deploy tiny apps
- Self-serve access for developers makes deploying internal services and one-off projects completely painless for everyone
- Easily deploy existing open source applications
- Saves money by not using one or more VM per service component
- Best practices included for HA, log aggregation
- Provides a standardized environment for the Ops team to manage
- Simple and composable vs existing Open Source PaaS products
