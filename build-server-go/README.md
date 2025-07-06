# Build Server (Go)

This is a Go implementation of the build server that replaces the Node.js version. It handles building projects and uploading the results to S3.

## Features

- Executes build commands (npm install && npm run build)
- Publishes build logs to Redis in real-time
- Uploads built files to AWS S3
- Handles build failures gracefully

## Prerequisites

- Go 1.21 or later
- Node.js and npm (for building frontend projects)
- Redis server
- AWS S3 access

## Environment Variables

Set the following environment variables:

```bash
PROJECT_ID=your-project-id
AWS_ACCESS_KEY_ID=your-aws-access-key
AWS_SECRET_ACCESS_KEY=your-aws-secret-key
AWS_REGION=your-aws-region
REDIS_URL=redis://localhost:6379
```

## Configuration

Update the following in `main.go`:

1. Redis connection details in `NewBuildServer()`
2. AWS credentials and region in `NewBuildServer()`
3. S3 bucket name in `uploadFiles()` (currently set to "vercel-clone-outputs")

## Installation

```bash
go mod download
go build -o build-server .
```

## Usage

```bash
# Set environment variables
export PROJECT_ID=my-project-123

# Run the build server
./build-server
```

## Docker Usage

```bash
# Build the Docker image
docker build -t build-server-go .

# Run the container
docker run -e PROJECT_ID=my-project-123 \
           -e AWS_ACCESS_KEY_ID=your-key \
           -e AWS_SECRET_ACCESS_KEY=your-secret \
           -e AWS_REGION=your-region \
           build-server-go
```

## How it works

1. The server starts and reads the PROJECT_ID from environment variables
2. It executes `npm install && npm run build` in the `./output` directory
3. Build logs are published to Redis channel `logs:{PROJECT_ID}`
4. After successful build, it uploads all files from `./output/dist` to S3
5. Files are uploaded to S3 with the key pattern: `__outputs/{PROJECT_ID}/{filename}`

## Dependencies

- `github.com/aws/aws-sdk-go-v2` - AWS SDK for S3 operations
- `github.com/go-redis/redis/v8` - Redis client for publishing logs