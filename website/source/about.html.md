---
title: The Story of Flynn
---

# The Story of Flynn

We ran into a huge problem when we started working on [Tent](https://tent.io): Operations.

### Scaling is Hard

We had designed Tent around services. Instead of one giant app, we had lots of smaller, individual components. There were also the other internal services we needed, like version control and chat servers, and maintenance was overwhelming our tiny team. It was taking longer to deploy apps to AWS or our own servers than to write them.

We knew we wanted something like Heroku that we could host ourselves, but we also knew that a Heroku clone wasn't enough. We needed something designed for today's needs. Our problem wasn't just deploying stateless web apps. We also needed to scale them, provision clusters, manage databases, and run anything that could run on Linux. A single platform that could do all of that would be great, but did it exist?

We looked at many off-the-shelf products, but we couldn't find the flexibility we wanted. We also didn't see a product that let you scale in any direction an app or growth required. We didn't want to get stuck with a product we couldn't deploy across regions or scale across servers.

Having worked closely with Ops teams in other startups, we understood the pressures they faced. They were losing touch with what the rest of the company was working on because they were so busy building and managing individual versions of applications. A single platform could simplify development, testing, and production for the rest of the organization.

### Flynn's Building Blocks

As we were starting to think about how to solve this problem, Google published the Omega paper. It demonstrated the power of using a resource framework as the basis for an internal platform. It also explained the shortcomings of the Borg/Mesos model which was designed for ephemeral map-reduce and big data workloads. Omega was far better suited to handle long-running processes like applications and databases. But as much as we liked Omega, we realized that higher-level abstractions and interfaces would be needed for problems like service discovery, deployment, and database management.

We also knew that many other companies and developers faced the same problems and dreamed of solutions. We spoke to friends and former employers to see who was interested in working together to build a solution. We initially suggested a co-operative effort where engineers from different companies would build different components to a common spec. While lots of people liked the idea, most companies didn't have the engineering resources to donate to the project. But by designing it for this collaboration,  Flynn is API-driven, highly-modular, and nearly every component is interchangeable

Several companies suggested another idea: if they sponsored the project, our team could focus on building it full time. [Shopify](https://shopify.com) was the first to make a significant contribution. Once we had the budget for a small team, we were able to invite long-time friend [Jeff Lindsay](http://progrium.com), who had been thinking about the ideal platform for years, to help design the spec and work on the proof of concept. Jeff introduced us to dotCloud who shared their plans for what would become [Docker](https://docker.com). We saw the potential value to the community and urged them both to write it in Go instead of Python and open source the entire project as early as possible.

### The Open-Source Community

When we needed a better way to work with backers of the project, we created a fundraising page with [Selfstarter](http://selfstarter.us). With the help of the community at [Hacker News](https://news.ycombinator.com), Flynn raised almost $100,000. We embraced open development and, by October 2013, we demoed a proof of concept to our first community Meetup.

In April 2014, we launched the first alpha release alongside a web dashboard for cluster management. The community's interest in Flynn was overwhelming! We thought the project would be great for startups, but many developers and large companies began following Flynn. Some corporations were unable to sponsor our work because of their organizational structures, but they offered to pay for services built around Flynn instead. As a result, we moved Flynn into a new company where it could grow independently of our other projects.

###Y-Combinator

In May 2014 we were accepted into the [Y Combinator](https://www.ycombinator.com) summer batch. With this funding, we were able to pay some of our active contributors and speed up Flynn's development.

This summer, our team has focused on stability instead of feature development so we could move Flynn into beta as soon as possible. After running into stability issues with Docker, we made Flynn's container system modular to support more mature solutions, starting with Red Hat's [libvirt](http://libvirt.org).

We expect Flynn to be production-stable later this fall, after which new feature development will continue. Our initial focus will be metrics, log aggregation, and appliances for other major open source databases.

We are also raising a round of venture capital which will allow us to expand the team even further and speed up the development timeline.
