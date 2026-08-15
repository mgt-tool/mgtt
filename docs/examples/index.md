# Worked examples

Complete walkthroughs against realistic systems. Each one shows the model, the scenarios, the CI wiring, and the refinements that came from running against a real cluster — not a toy stack.

## Available

**[Blue/green storefront on EKS](blue-green-storefront.md)** — PHP-FPM behind nginx, blue/green traffic switching, async queue tier, cron, AWS-managed data layer (RDS + ElastiCache + AmazonMQ + S3 + CloudFront) + ESO reconciling config from SSM. 20 components, 5 scenarios, a section of lessons learned from the first few weeks of use.

*Read this one if you operate anything Kubernetes-on-AWS with a colored deploy model.*

## Seeing something simpler first?

The [Quick Start](../getting-started/quickstart.md) walks through a four-component system (nginx → frontend + api → rds) end-to-end in five minutes. It's the right entry point before the examples here.
