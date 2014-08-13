---
title: The Story of Flynn
---

# The Story of Flynn

We ran into a huge problem when we started working on [Tent](https://tent.io):
ops.

We had embraced service oriented architecture which meant instead of one
monolith, we had lots of smaller individual components. Combined with internal
services like version control and chat servers, our tiny team was overwhelmed.
It regularly took longer to deploy apps to AWS or our own servers than to write
them.

We knew we wanted something like Heroku that we could host ourselves, but we
also knew that a Heroku clone wasn't enough. We needed something suited to
today's needs. It wasn't just about deploying stateless web apps, it was scaling
them, provisioning and managing databases, and ideally the ability to run
anything that could run on Linux all inside a single platform.

We looked at many off the shelf products, but none had the flexibility we wanted
or solved the intersection of scale up and scale out.

Having observed and interacted closely with ops teams in other startups, we knew
the pressure they faced. We wanted to build something that would transform
operations from a group of consultants into a product team. Ops could manage
a single platform that provided self-serve resources for development, testing,
and production to the rest of the organization.

Google published the Omega paper just as we were starting to think seriously
about building something to solve this problem. It demonstrated the power of
using a resource framework as the basis for an internal platform. It also
explained the shortcomings of the Borg/Mesos model which was designed for
ephemeral map-reduce and big data workloads and why Omega was far better suited
to handle long running processes like applications and databases. As much as we
liked Omega, we realized that higher level abstractions and interfaces would be
needed for problems like service discovery, deployment, and database management.

We also knew that many other companies and developers faced the same problems
and dreamed of a similar solution. We reached out to friends and former
employers to see who was interested in working together to build a solution. We
initially suggested a co-operative effort where engineers from different
companies would build different components to a common spec. While lots of
people liked the idea, most companies didn't have the engineering resources to
spare. The legacy of this design choice is that Flynn is API-driven, highly
modular, and nearly every component is interchangeable

Several companies suggested an alternative: if they supported the project
financially we could focus on building it full time.
[Shopify](https://shopify.com) was the first to make a significant contribution.
Once we had the budget for a small team we were able to invite long-time friend
[Jeff Lindsay](http://progrium.com), who had been thinking about the ideal PaaS
for years, to help design the spec and work on the proof of concept. Jeff
introduced us to dotCloud who shared their plans for what would become
[Docker](https://docker.com). We saw the potential value to the community and
urged them both to write it in Go instead of Python and open source the entire
project as early as possible.

Almost as an afterthought we created a fundraising page based on
[Selfstarter](http://selfstarter.us) which raised almost $100,000, mostly
through [Hacker News](https://news.ycombinator.com). We embraced open
development and by October we demoed a proof of concept to our first community
meetup.

In April we launched the first alpha release alongside a web management tool. We
were overwhelmed by community interest in Flynn. We thought the project would
appeal primarily to startups, but a tremendous number of individuals and large
enterprises began following our progress. Many of the larger companies were
unable to sponsor our work because of their organizational structures, but some
offered to pay for services around Flynn instead. As a result we moved Flynn
into a new company where it could grow independently of our other projects.

In May we were accepted into the [Y Combinator](http://libvirt.org) summer
batch. With this additional funding we were able to pay some of our most active
contributors to increase work even more on Flynn.

This summer our team has focused on stability instead of feature development so
we could move Flynn into beta as soon as possible. After running into stability
issues with Docker, we modularized Flynn's containerization system to support
more mature solutions, starting with Red Hat's [libvirt](http://libvirt.org).

We expect Flynn to be production stable this fall, after which new feature
development will continue, starting with metrics, log aggregation, and
appliances for other major open source databases.

We are also raising a round of venture capital which will allow us to expand the
team even further and speed up the development timeline.
