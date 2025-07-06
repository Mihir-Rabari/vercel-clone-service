# API Server (Go)

This is a Go implementation of the API server that replaces the Node.js version. It provides REST API endpoints and WebSocket connections for real-time build logs.

## Features

- REST API for creating build projects
- WebSocket support for real-time build logs
- AWS ECS integration for running build tasks
- Redis integration for log streaming
- CORS support for frontend integration

## Prerequisites

- Go 1.21 or later
- Redis server
- AWS ECS cluster and task definition
- AWS credentials with ECS permissions

## Environment Variables

Set the following environment variables:

```bash
PORT=9000
AWS_ACCESS_KEY_ID=your-aws-access-key
AWS_SECRET_ACCESS_KEY=your-aws-secret-key
AWS_REGION=your-aws-region
REDIS_URL=redis://localhost:6379
```

## Configuration

Update the following in `main.go`:

1. Redis connection details in `NewAPIServer()`
2. AWS credentials and region in `NewAPIServer()`
3. ECS cluster, task definition, subnets, and security groups in `AWSConfig`

## Installation

```bash
go mod download
go build -o api-server .
```

## Usage

```bash
# Run the API server
./api-server
```

## Docker Usage

```bash
# Build the Docker image
docker build -t api-server-go .

# Run the container
docker run -p 9000:9000 \
           -e AWS_ACCESS_KEY_ID=your-key \
           -e AWS_SECRET_ACCESS_KEY=your-secret \
           -e AWS_REGION=your-region \
           api-server-go
```

## API Endpoints

### POST /project

Creates a new build project and starts an ECS task.

**Request:**
```json
{
  "gitURL": "https://github.com/user/repo.git",
  "slug": "optional-custom-slug"
}
```

**Response:**
```json
{
  "status": "queued",
  "data": {
    "projectSlug": "generated-or-custom-slug",
    "url": "http://generated-slug.localhost:8000"
  }
}
```

### GET /ws

WebSocket endpoint for real-time build logs.

**Usage:**
```javascript
const ws = new WebSocket('ws://localhost:9000/ws');

ws.onopen = () => {
  // Subscribe to logs for a specific project
  ws.send(JSON.stringify({
    action: 'subscribe',
    channel: 'logs:project-slug'
  }));
};

ws.onmessage = (event) => {
  const data = JSON.parse(event.data);
  console.log('Build log:', data.message);
};
```

## How it works

1. The API server starts and connects to Redis and AWS ECS
2. When a POST request is made to `/project`, it:
   - Generates a project slug (if not provided)
   - Creates an ECS task with the git URL and project ID as environment variables
   - Returns the project details
3. WebSocket connections can subscribe to Redis channels for real-time logs
4. Build logs published to Redis are forwarded to connected WebSocket clients

## Dependencies

- `github.com/gin-gonic/gin` - HTTP web framework
- `github.com/gorilla/websocket` - WebSocket implementation
- `github.com/aws/aws-sdk-go-v2/service/ecs` - AWS ECS client
- `github.com/go-redis/redis/v8` - Redis client
- `github.com/google/uuid` - UUID generation for project slugs