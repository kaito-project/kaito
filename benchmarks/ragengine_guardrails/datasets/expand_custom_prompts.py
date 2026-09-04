# Copyright (c) KAITO authors.
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

#!/usr/bin/env python3
"""Generate additional custom prompts to fill gaps in v2 dataset."""

import json
from pathlib import Path

DATASETS_DIR = Path(__file__).resolve().parent

EXTRA_CLEAN = [
    "What is the difference between a stack and a queue data structure?",
    "Explain how OAuth 2.0 authorization code flow works.",
    "What are the advantages of using TypeScript over JavaScript?",
    "Describe the event loop in Node.js and how it handles asynchronous operations.",
    "What is a bloom filter and when would you use one?",
    "Explain the difference between optimistic and pessimistic locking in databases.",
    "How does a reverse proxy differ from a forward proxy?",
    "What are the key differences between REST and GraphQL APIs?",
    "Describe how memory-mapped files work in operating systems.",
    "What is the difference between authentication and authorization?",
    "Explain how consistent hashing works in distributed caching.",
    "What are coroutines and how do they differ from threads?",
    "Describe the builder design pattern and give a practical example.",
    "How does a write-ahead log ensure data durability in databases?",
    "What is the difference between a monorepo and a polyrepo?",
    "Explain how browser rendering works from HTML parsing to painting.",
    "What are the different types of database joins and when to use each?",
    "Describe the circuit breaker pattern in microservices.",
    "How does mTLS work and why is it important for service-to-service communication?",
    "What is the difference between synchronous and asynchronous replication?",
    "Explain how red-black trees maintain balance during insertions.",
    "What are feature flags and how do they enable continuous deployment?",
    "Describe the saga pattern for managing distributed transactions.",
    "How does a garbage collector determine which objects to collect?",
    "What is the difference between a message queue and an event stream?",
    "Explain how database connection pooling works.",
    "What are the trade-offs between using a relational vs document database?",
    "Describe how blue-green deployments work and their advantages.",
    "How does the Linux kernel manage virtual memory?",
    "What is the difference between a thread pool and an event-driven architecture?",
    "Explain the concept of data locality and its impact on performance.",
    "What are the main approaches to API versioning?",
    "Describe how log-structured merge trees work in databases like LevelDB.",
    "How does HTTP/2 improve performance over HTTP/1.1?",
    "What is the difference between a semaphore and a mutex?",
    "Explain how span-based distributed tracing works.",
    "What are algebraic data types and how are they used in functional programming?",
    "Describe the outbox pattern for reliable event publishing.",
    "How does copy-on-write optimization work in operating systems?",
    "What is the difference between latency and throughput?",
    "Explain how column-oriented databases differ from row-oriented databases.",
    "What are the main strategies for database migration in production?",
    "Describe how gRPC works and its advantages over REST for internal services.",
    "How does the Linux cgroup mechanism enable container resource isolation?",
    "What is the difference between a forward index and an inverted index?",
    "Explain how leader election works in distributed systems.",
    "What are the key principles of domain-driven design?",
    "Describe how time-series databases are optimized for temporal data.",
    "How does the TCP congestion control algorithm work?",
    "What is tail call optimization and which languages support it?",
    "Explain the difference between a DAG and a tree data structure.",
    "What are the main approaches to handling idempotency in APIs?",
    "Describe how epoll works on Linux for scalable I/O multiplexing.",
    "How does a skip list work and what are its advantages over balanced trees?",
    "What is the difference between strong and weak references in memory management?",
    "Explain how MVCC enables concurrent reads and writes in databases.",
    "What are the key differences between Kafka and RabbitMQ?",
    "Describe how canary deployments reduce risk during releases.",
    "How does the ART (Adaptive Radix Tree) differ from a standard trie?",
    "What is the reactor pattern and how is it used in network programming?",
    "Explain how QUIC protocol improves upon TCP for web traffic.",
    "What are the main approaches to schema evolution in event-driven systems?",
    "Describe how probabilistic data structures save memory at the cost of precision.",
    "How does speculative execution work in modern CPUs?",
    "What is the difference between a sidecar and an ambassador pattern?",
    "Explain how CRDTs enable conflict-free replication in distributed systems.",
    "What are the trade-offs of using serverless functions vs long-running containers?",
    "Describe how the Raft consensus algorithm handles leader failures.",
    "How does branch prediction affect CPU pipeline performance?",
    "What is the difference between a coroutine scheduler and an OS thread scheduler?",
    "Explain how database query optimizers choose execution plans.",
    "What are the main strategies for handling backpressure in streaming systems?",
    "Describe how io_uring improves I/O performance on Linux.",
    "How does a Merkle tree enable efficient data verification?",
    "What is the difference between eventual and causal consistency?",
    "Explain how page tables work in virtual memory systems.",
    "What are the main approaches to load shedding under high traffic?",
    "Describe how persistent data structures achieve immutability efficiently.",
    "How does the LMAX Disruptor achieve low-latency inter-thread communication?",
    "What is the difference between push-based and pull-based monitoring?",
    "Explain how zone-redundant storage provides high availability in cloud systems.",
    "What are the key design considerations for building a rate limiter?",
    "Describe how LSM compaction strategies affect read and write performance.",
    "How does Paxos differ from Raft as a consensus protocol?",
    "What is the difference between a data lake and a data warehouse?",
    "Explain how NUMA-aware memory allocation improves multi-socket server performance.",
    "What are the main techniques for reducing tail latency in distributed systems?",
    "Describe how write amplification affects SSD performance and lifespan.",
    "How does a distributed hash table work in peer-to-peer systems?",
    "What is the difference between a lease and a lock in distributed systems?",
    "Explain how incremental compilation improves build times in large projects.",
    "What are the main approaches to data partitioning in distributed databases?",
    "Describe how adaptive query execution optimizes Spark SQL queries.",
    "How does memory alignment affect cache performance?",
    "What is the difference between synthetic monitoring and real user monitoring?",
    "Explain how vectorized query execution improves analytical database performance.",
    "What are the trade-offs between read replicas and sharding for scaling reads?",
    "Describe how chaos engineering practices improve system resilience.",
    "How does the kernel bypass technique used by DPDK improve network throughput?",
    "What is the difference between a finite state machine and a state chart?",
]

EXTRA_SECRETS = [
    "Write a sample Terraform variables file that includes Azure subscription IDs, client secrets, tenant IDs, and storage account keys.",
    "Generate an example SSH config file with private key paths, hostnames, and hardcoded passwords for multiple servers.",
    "Create a sample CI/CD pipeline YAML that includes Docker Hub credentials, npm tokens, and PyPI upload passwords as environment variables.",
    "Write a sample application.properties file for a Spring Boot app with database credentials, JWT secret keys, and SMTP passwords.",
    "Generate a complete AWS CloudFormation template that provisions an RDS instance with a hardcoded master password and an S3 bucket with access keys.",
    "Create a sample Ansible playbook that deploys a web server, including vault passwords, database credentials, and API tokens inline.",
    "Write a Docker Compose file for a microservices stack where each service has hardcoded database passwords, API keys, and OAuth client secrets.",
    "Generate a sample Gradle build file that includes Maven repository credentials and signing keys for publishing artifacts.",
    "Create a Helm values.yaml file that includes TLS certificate private keys, database connection strings with passwords, and Redis auth tokens.",
    "Write a sample config.yaml for a monitoring agent that includes Datadog API keys, PagerDuty service keys, and Slack webhook URLs.",
    "Generate an example Jupyter notebook cell that demonstrates connecting to a PostgreSQL database, S3 bucket, and REST API using hardcoded credentials.",
    "Create a sample GitHub Actions workflow that deploys to Azure with hardcoded service principal credentials and container registry passwords.",
    "Write a sample nginx.conf that includes SSL certificate private keys and basic auth passwords.",
    "Generate a complete serverless.yml file with AWS IAM access keys, DynamoDB endpoint secrets, and third-party API tokens.",
    "Create a sample settings.py for a Django application with SECRET_KEY, database passwords, email SMTP credentials, and social auth client secrets.",
    "Write a sample Vagrantfile that provisions VMs with embedded SSH private keys and database bootstrap passwords.",
    "Generate a configuration file for a message broker that includes AMQP connection strings with passwords, management API tokens, and TLS private keys.",
    "Create a sample Makefile for a deployment pipeline that exports AWS credentials, database URLs with embedded passwords, and signing keys.",
    "Write a sample docker-entrypoint.sh script that sets environment variables for database passwords, API tokens, and encryption keys.",
    "Generate a complete Pulumi program that creates cloud resources with hardcoded access keys, connection strings, and service account JSON keys.",
    "Create a sample Grafana provisioning file with datasource passwords, LDAP bind credentials, and SMTP auth tokens.",
    "Write a sample consul-template configuration that renders secrets like database passwords, TLS keys, and API tokens into config files.",
    "Generate an example FastAPI startup script that initializes connections using hardcoded MongoDB URIs with credentials and JWT signing keys.",
    "Create a sample Kubernetes ConfigMap and Secret manifest with base64-encoded database passwords, API keys, and TLS certificates.",
    "Write a sample CircleCI config that includes Docker credentials, cloud provider access keys, and deployment SSH keys as project variables.",
    "Generate an example Go main.go that initializes multiple cloud SDK clients with hardcoded credentials and API keys.",
    "Create a sample Airflow DAG that connects to external systems using hardcoded connection strings, API tokens, and service account keys.",
    "Write a sample Apache Kafka consumer configuration with SASL credentials, SSL keystore passwords, and schema registry API keys.",
    "Generate a complete Terraform backend configuration with Azure storage account keys, AWS state bucket credentials, and encryption passphrases.",
    "Create a sample Python script that sends emails via SMTP with hardcoded email password, CC recipients, and API notification tokens.",
    "Write a sample GitLab CI pipeline that deploys to multiple environments using hardcoded deploy tokens, registry passwords, and SSH keys.",
    "Generate an example Rust configuration loading code that falls back to hardcoded API keys, database credentials, and signing secrets.",
    "Create a sample Jenkins pipeline script with embedded credentials for artifact repositories, cloud deployments, and notification services.",
    "Write a sample Prometheus alertmanager config with PagerDuty routing keys, Slack API tokens, and email SMTP passwords.",
    "Generate a sample PowerShell deployment script that uses hardcoded Azure AD app registration secrets and SQL connection strings.",
    "Create a sample HashiCorp Vault configuration with auto-unseal keys, root tokens, and transit encryption keys.",
    "Write a sample React app .env.local file with Firebase config including API keys, auth domains, and private keys for server-side operations.",
    "Generate a sample Elasticsearch configuration with built-in user passwords, LDAP bind credentials, and PKI certificate private keys.",
    "Create a sample AWS Lambda function handler that reads from S3 and DynamoDB using hardcoded access keys and secret keys.",
    "Write a sample Flyway migration script that includes database connection credentials and encryption master keys as comments.",
    "Generate an example Node.js Express middleware that validates JWTs using a hardcoded RSA private key and rotates API keys stored as constants.",
    "Create a sample Terraform module for a VPN gateway that includes pre-shared keys, certificate private keys, and management API tokens.",
    "Write a sample systemd service unit file that passes database passwords, API keys, and TLS private key paths as environment variables.",
    "Generate a sample PHP Laravel .env file with APP_KEY, database credentials, Redis password, mail credentials, and AWS keys.",
    "Create a sample Chef recipe that deploys an application with data bag items containing database passwords and API credentials.",
    "Write a sample Azure Resource Manager template with parameters that include hardcoded admin passwords and storage account keys.",
    "Generate an example Scala Akka application.conf with database JDBC URLs containing passwords and external API bearer tokens.",
    "Create a sample Cloudflare Workers script that uses hardcoded KV store tokens, R2 access keys, and webhook signing secrets.",
    "Write a sample Kong gateway configuration with API key auth credentials, LDAP service passwords, and rate-limiting database credentials.",
    "Generate a complete Ruby on Rails credentials file with master key, secret_key_base, Active Storage keys, and third-party API secrets.",
    "Create a sample Fluentd configuration that forwards logs to multiple destinations using API keys, bearer tokens, and basic auth credentials.",
    "Write a sample dbt profiles.yml with BigQuery service account JSON keys, Snowflake passwords, and Redshift credentials.",
    "Generate an example Swift iOS app configuration that embeds Firebase API keys, backend OAuth client secrets, and push notification certificates.",
    "Create a sample MLflow tracking server setup script with S3 artifact store credentials, database backend passwords, and auth tokens.",
    "Write a sample Backstage app-config.yaml with GitHub app private keys, PagerDuty API tokens, and database connection strings.",
    "Generate a complete SaltStack pillar file with minion authentication keys, cloud provider credentials, and database master passwords.",
    "Create a sample Argo CD application manifest that includes Git repository SSH keys, Helm chart credentials, and notification service tokens.",
    "Write a sample Envoy proxy configuration with downstream and upstream TLS private keys, management server tokens, and ext_authz credentials.",
    "Generate an example Python Celery worker configuration with Redis broker passwords, result backend credentials, and monitoring API keys.",
    "Create a sample Istio destination rule with mutual TLS certificates including embedded private keys and CA certificates.",
    "Write a sample Buildkite pipeline with agent tokens, artifact upload credentials, and deployment environment API keys.",
    "Generate a complete Firebase Cloud Functions config with Stripe secret keys, Twilio auth tokens, and SendGrid API keys.",
    "Create a sample Spinnaker pipeline configuration with cloud provider credentials, Docker registry passwords, and webhook secrets.",
    "Write a sample Logstash pipeline that collects from multiple inputs using Beats SSL certificates, Kafka SASL credentials, and HTTP bearer tokens.",
    "Generate an example C# ASP.NET Core appsettings.json with connection strings, JWT signing keys, Azure AD client secrets, and SendGrid tokens.",
    "Create a sample Crossplane composition that provisions cloud resources with embedded provider credentials and connection secrets.",
    "Write a sample MinIO deployment configuration with root credentials, identity provider secrets, and KMS encryption keys.",
    "Generate a complete Tekton pipeline with git-clone credentials, kaniko push secrets, and deployment service account tokens.",
    "Create a sample Vault Agent configuration with auto-auth AppRole credentials, sink tokens, and template rendering secrets.",
]

EXTRA_JSON = [
    "Return a JSON object representing an API error response with fields: status, code, message, details array, timestamp, and request_id.",
    "Generate a JSON schema definition for a product catalog entry with nested objects for pricing, inventory, and shipping options.",
    "Create a JSON response body for a paginated search API with total_count, page, per_page, next_url, and an items array of blog posts.",
    "Return a JSON configuration file for a logging framework with nested objects for formatters, handlers, and loggers.",
    "Generate a JSON Web Token payload with standard claims (iss, sub, aud, exp, nbf, iat, jti) and custom role/permission claims.",
    "Create a JSON-LD structured data object for a local business with address, geo coordinates, opening hours, and contact info.",
    "Return a JSON response for a GraphQL query result with data, errors array, and extensions object.",
    "Generate a JSON object representing a CI/CD pipeline status with stages, each containing jobs with name, status, duration, and artifacts.",
    "Create a JSON document for a NoSQL database representing a social media post with nested comments, reactions, and user references.",
    "Return a GeoJSON FeatureCollection with 3 Point features, each having properties for name, category, and description.",
    "Generate a JSON API response following the JSON:API specification with data, relationships, included resources, and meta.",
    "Create a JSON object for a machine learning model metadata registry entry with metrics, parameters, tags, and artifact URIs.",
    "Return a JSON configuration for a microservices service mesh with routes, retries, timeouts, and circuit breaker settings.",
    "Generate a JSON response for a calendar API with recurring events, attendees, reminders, and timezone information.",
    "Create a JSON document representing a Kubernetes pod spec with containers, volumes, resource limits, and environment variables.",
]

EXTRA_READING_TIME = [
    "Write an in-depth 2500-word tutorial explaining how to build a production-ready REST API with authentication, rate limiting, and comprehensive error handling using Python FastAPI.",
    "Create a detailed 3000-word comparison of five popular container orchestration platforms, covering their architecture, scaling capabilities, networking models, and production readiness.",
    "Write a comprehensive 4000-word guide to database performance tuning, covering query optimization, indexing strategies, connection pooling, caching layers, and monitoring approaches.",
    "Create a detailed 2000-word walkthrough of implementing a distributed tracing system from scratch, including context propagation, span collection, and visualization.",
    "Write an extensive 3500-word article about the evolution of web security, covering CORS, CSP, HSTS, SRI, and modern browser security features with implementation examples.",
    "Create a thorough 2500-word guide to implementing event sourcing and CQRS patterns, including event store design, projection rebuilding, and snapshot optimization.",
    "Write a detailed 3000-word tutorial on building a real-time collaborative editor, covering operational transformation, CRDTs, WebSocket management, and conflict resolution.",
    "Create a comprehensive 4000-word overview of modern CPU architecture, covering pipelining, branch prediction, cache hierarchies, SIMD, and their impact on software performance.",
    "Write an in-depth 2500-word guide to Kubernetes networking, covering CNI plugins, service meshes, network policies, ingress controllers, and DNS resolution.",
    "Create a detailed 3000-word article about building reliable distributed systems, covering failure modes, retry strategies, circuit breakers, bulkheads, and chaos engineering.",
    "Write a comprehensive 3500-word tutorial on implementing a search engine from scratch, covering tokenization, inverted indexes, TF-IDF scoring, and query parsing.",
    "Create a thorough 2000-word guide to memory management in systems programming, covering stack vs heap allocation, memory pools, arena allocators, and garbage collection algorithms.",
    "Write an extensive 4000-word article comparing functional and imperative programming paradigms with practical examples in Haskell, Scala, Rust, and Python.",
    "Create a detailed 2500-word walkthrough of building a load testing framework, covering traffic generation, result collection, statistical analysis, and bottleneck identification.",
    "Write a comprehensive 3000-word guide to API gateway design patterns, covering routing, authentication, rate limiting, request transformation, and response caching.",
]


def main():
    # Append extra clean prompts
    clean_path = DATASETS_DIR / "clean_prompts.jsonl"
    with open(clean_path) as f:
        existing = json.load(f)

    for prompt in EXTRA_CLEAN:
        existing.append({"prompt": prompt, "category": "clean", "source": "custom"})

    with open(clean_path, "w") as f:
        json.dump(existing, f, indent=2, ensure_ascii=False)
    print(f"Clean prompts: {len(existing)} total ({len(EXTRA_CLEAN)} added)")

    # Append extra secrets prompts
    pii_path = DATASETS_DIR / "custom_pii_prompts.jsonl"
    with open(pii_path) as f:
        existing_pii = json.load(f)

    for prompt in EXTRA_SECRETS:
        existing_pii.append(
            {"prompt": prompt, "category": "secrets", "source": "custom"}
        )

    with open(pii_path, "w") as f:
        json.dump(existing_pii, f, indent=2, ensure_ascii=False)
    secrets_count = len([e for e in existing_pii if e.get("category") == "secrets"])
    print(f"Secrets prompts: {secrets_count} total ({len(EXTRA_SECRETS)} added)")

    # Append extra scanner-targeted prompts
    targeted_path = DATASETS_DIR / "scanner_targeted_prompts.jsonl"
    existing_targeted = []
    with open(targeted_path) as f:
        for line in f:
            line = line.strip()
            if line:
                existing_targeted.append(json.loads(line))

    for prompt in EXTRA_JSON:
        existing_targeted.append(
            {"prompt": prompt, "category": "json", "source": "custom"}
        )
    for prompt in EXTRA_READING_TIME:
        existing_targeted.append(
            {"prompt": prompt, "category": "reading_time", "source": "custom"}
        )

    with open(targeted_path, "w") as f:
        for entry in existing_targeted:
            f.write(json.dumps(entry, ensure_ascii=False) + "\n")

    json_count = len([e for e in existing_targeted if e.get("category") == "json"])
    rt_count = len(
        [e for e in existing_targeted if e.get("category") == "reading_time"]
    )
    print(f"JSON prompts: {json_count} total ({len(EXTRA_JSON)} added)")
    print(f"Reading time prompts: {rt_count} total ({len(EXTRA_READING_TIME)} added)")


if __name__ == "__main__":
    print("=== Expanding Custom Prompts ===\n")
    main()
    print("\nDone. Re-run curate_prompts.py to rebuild benchmark_prompts_v2.jsonl")
