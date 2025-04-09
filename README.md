# Airflow Observability Platform

A comprehensive observability and monitoring solution for Apache Airflow, designed to provide real-time insights into your data pipeline health, performance, and compliance.

## Features

- **Real-time Monitoring Dashboard**
  - DAG and task execution metrics
  - Pipeline health status
  - Compliance rate tracking
  - Violation detection and alerting

- **Advanced Metrics Collection**
  - Task and DAG lifecycle event tracking
  - Execution duration monitoring
  - Success/failure rate analysis
  - Consecutive failure detection

- **SLA Management**
  - Customizable SLA rules
  - Violation detection and reporting
  - Severity-based alerting
  - Compliance rate calculation

- **Data Lineage Tracking**
  - End-to-end pipeline visibility
  - Input/output dependency tracking
  - Job type and state monitoring
  - Run-level state tracking

## Getting Started

### Prerequisites

- Go 1.23.3 or higher
- Docker and Docker Compose
- Apache Airflow instance

### Installation

1. Clone the repository:
```bash
git clone https://github.com/yourusername/airflow-observer.git
cd airflow-observer
```

2. Build the project:
```bash
make build
```

3. Run the application:
```bash
make run
```

4. Build and run with Docker:
```bash
make image
docker-compose up
```

### Configuration

1. Configure your Airflow instance to use the metrics collector plugin
2. Set the environment variable `METRICS_SERVICE_URL` to point to your observer instance
3. Access the web interface at `http://localhost:8000`

## Project Structure

```
.
├── handlers/         # HTTP request handlers
├── models/          # Data models and types
├── services/        # Business logic
├── storage/         # Data storage implementation
├── templates/       # HTML templates
├── main.go          # Application entry point
├── Makefile         # Build and deployment commands
├── Dockerfile       # Docker configuration
└── docker-compose.yaml # Docker Compose configuration
```

## Usage

### Monitoring Dashboard

Access the dashboard at `http://localhost:8000/dashboard` to view:
- Overall pipeline health
- Compliance rates
- Violation statistics
- Top performing DAGs

### Metrics View

View detailed metrics at `http://localhost:8000/metrics` including:
- Task execution times
- Success/failure rates
- Duration statistics
- Readiness scores

### SLA Management

Configure and manage SLAs at `http://localhost:8000/rules`:
- Create custom rules
- Set severity levels
- Monitor violations
- Track compliance

