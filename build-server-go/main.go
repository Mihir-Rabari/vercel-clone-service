package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-redis/redis/v8"
)

type BuildServer struct {
	s3Client  *s3.Client
	publisher *redis.Client
	projectID string
}

func NewBuildServer() (*BuildServer, error) {
	// Get configuration from environment variables
	redisAddr := os.Getenv("REDIS_URL")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	
	// Initialize Redis client
	rdb := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: os.Getenv("REDIS_PASSWORD"),
		DB:       0,
	})

	// Initialize AWS S3 client
	awsRegion := os.Getenv("AWS_REGION")
	if awsRegion == "" {
		awsRegion = "us-east-1"
	}
	
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion(awsRegion),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	s3Client := s3.NewFromConfig(cfg)

	projectID := os.Getenv("PROJECT_ID")
	if projectID == "" {
		return nil, fmt.Errorf("PROJECT_ID environment variable is required")
	}

	return &BuildServer{
		s3Client:  s3Client,
		publisher: rdb,
		projectID: projectID,
	}, nil
}

func (bs *BuildServer) publishLog(message string) {
	logMessage := fmt.Sprintf(`{"log":"%s"}`, message)
	channel := fmt.Sprintf("logs:%s", bs.projectID)
	
	err := bs.publisher.Publish(context.Background(), channel, logMessage).Err()
	if err != nil {
		log.Printf("Failed to publish log: %v", err)
	}
}

func (bs *BuildServer) runBuild() error {
	log.Println("Executing build script")
	bs.publishLog("Build Started...")

	outputDir := filepath.Join(".", "output")
	
	// Change to output directory and run npm install && npm run build
	cmd := exec.Command("sh", "-c", "npm install && npm run build")
	cmd.Dir = outputDir

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start command: %w", err)
	}

	// Read stdout
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				output := string(buf[:n])
				log.Print(output)
				bs.publishLog(output)
			}
			if err != nil {
				break
			}
		}
	}()

	// Read stderr
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				output := string(buf[:n])
				log.Printf("Error: %s", output)
				bs.publishLog(fmt.Sprintf("error: %s", output))
			}
			if err != nil {
				break
			}
		}
	}()

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("build command failed: %w", err)
	}

	log.Println("Build Complete")
	bs.publishLog("Build Complete")

	// Upload files to S3
	return bs.uploadFiles()
}

func (bs *BuildServer) uploadFiles() error {
	distPath := filepath.Join(".", "output", "dist")
	bs.publishLog("Starting to upload")

	err := filepath.Walk(distPath, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// Get relative path from dist folder
		relPath, err := filepath.Rel(distPath, filePath)
		if err != nil {
			return err
		}

		log.Printf("uploading %s", filePath)
		bs.publishLog(fmt.Sprintf("uploading %s", relPath))

		// Open file
		file, err := os.Open(filePath)
		if err != nil {
			return fmt.Errorf("failed to open file %s: %w", filePath, err)
		}
		defer file.Close()

		// Determine content type
		contentType := mime.TypeByExtension(filepath.Ext(filePath))
		if contentType == "" {
			contentType = "application/octet-stream"
		}

		// Upload to S3
		bucket := os.Getenv("S3_BUCKET")
		if bucket == "" {
			bucket = "vercel-clone-outputs"
		}
		
		key := fmt.Sprintf("__outputs/%s/%s", bs.projectID, relPath)
		_, err = bs.s3Client.PutObject(context.Background(), &s3.PutObjectInput{
			Bucket:      aws.String(bucket),
			Key:         aws.String(key),
			Body:        file,
			ContentType: aws.String(contentType),
		})

		if err != nil {
			return fmt.Errorf("failed to upload file %s: %w", filePath, err)
		}

		bs.publishLog(fmt.Sprintf("uploaded %s", relPath))
		log.Printf("uploaded %s", filePath)

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to upload files: %w", err)
	}

	bs.publishLog("Done")
	log.Println("Done...")
	return nil
}

func main() {
	buildServer, err := NewBuildServer()
	if err != nil {
		log.Fatalf("Failed to initialize build server: %v", err)
	}

	if err := buildServer.runBuild(); err != nil {
		log.Fatalf("Build failed: %v", err)
	}
}