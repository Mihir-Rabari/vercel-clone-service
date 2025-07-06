package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/gorilla/websocket"
	"github.com/google/uuid"
)

type APIServer struct {
	ecsClient *ecs.Client
	subscriber *redis.Client
	config    AWSConfig
}

type AWSConfig struct {
	Cluster         string
	TaskDefinition  string
	Subnets         []string
	SecurityGroups  []string
}

type ProjectRequest struct {
	GitURL string `json:"gitURL" binding:"required"`
	Slug   string `json:"slug"`
}

type ProjectResponse struct {
	Status string      `json:"status"`
	Data   ProjectData `json:"data"`
}

type ProjectData struct {
	ProjectSlug string `json:"projectSlug"`
	URL         string `json:"url"`
}

type LogMessage struct {
	Log string `json:"log"`
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for CORS
	},
}

func NewAPIServer() (*APIServer, error) {
	// Get configuration from environment variables
	redisAddr := os.Getenv("REDIS_URL")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	
	// Initialize Redis client
	subscriber := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: os.Getenv("REDIS_PASSWORD"),
		DB:       0,
	})

	// Initialize AWS ECS client
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

	ecsClient := ecs.NewFromConfig(cfg)

	// Parse subnets and security groups from environment variables
	subnets := strings.Split(os.Getenv("ECS_SUBNETS"), ",")
	securityGroups := strings.Split(os.Getenv("ECS_SECURITY_GROUPS"), ",")
	
	awsConfig := AWSConfig{
		Cluster:         os.Getenv("ECS_CLUSTER"),
		TaskDefinition:  os.Getenv("ECS_TASK_DEFINITION"),
		Subnets:         subnets,
		SecurityGroups:  securityGroups,
	}

	return &APIServer{
		ecsClient:  ecsClient,
		subscriber: subscriber,
		config:     awsConfig,
	}, nil
}

func generateSlug() string {
	// Simple slug generation using UUID
	id := uuid.New().String()
	return strings.ReplaceAll(id[:8], "-", "")
}

func (api *APIServer) createProject(c *gin.Context) {
	var req ProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	projectSlug := req.Slug
	if projectSlug == "" {
		projectSlug = generateSlug()
	}

	// Create ECS task
	taskInput := &ecs.RunTaskInput{
		Cluster:        aws.String(api.config.Cluster),
		TaskDefinition: aws.String(api.config.TaskDefinition),
		LaunchType:     types.LaunchTypeFargate,
		Count:          aws.Int32(1),
		NetworkConfiguration: &types.NetworkConfiguration{
			AwsvpcConfiguration: &types.AwsVpcConfiguration{
				AssignPublicIp: types.AssignPublicIpEnabled,
				Subnets:        api.config.Subnets,
				SecurityGroups: api.config.SecurityGroups,
			},
		},
		Overrides: &types.TaskOverride{
			ContainerOverrides: []types.ContainerOverride{
				{
					Name: aws.String("builder-image"),
					Environment: []types.KeyValuePair{
						{
							Name:  aws.String("GIT_REPOSITORY__URL"),
							Value: aws.String(req.GitURL),
						},
						{
							Name:  aws.String("PROJECT_ID"),
							Value: aws.String(projectSlug),
						},
					},
				},
			},
		},
	}

	_, err := api.ecsClient.RunTask(context.Background(), taskInput)
	if err != nil {
		log.Printf("Failed to run ECS task: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start build"})
		return
	}

	response := ProjectResponse{
		Status: "queued",
		Data: ProjectData{
			ProjectSlug: projectSlug,
			URL:         fmt.Sprintf("http://%s.localhost:8000", projectSlug),
		},
	}

	c.JSON(http.StatusOK, response)
}

func (api *APIServer) handleWebSocket(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("Failed to upgrade connection: %v", err)
		return
	}
	defer conn.Close()

	// Handle WebSocket messages
	for {
		var msg map[string]interface{}
		err := conn.ReadJSON(&msg)
		if err != nil {
			log.Printf("WebSocket read error: %v", err)
			break
		}

		if action, ok := msg["action"].(string); ok && action == "subscribe" {
			if channel, ok := msg["channel"].(string); ok {
				// Subscribe to Redis channel and forward messages
				go api.subscribeAndForward(conn, channel)
				
				// Send confirmation
				conn.WriteJSON(map[string]string{
					"message": fmt.Sprintf("Joined %s", channel),
				})
			}
		}
	}
}

func (api *APIServer) subscribeAndForward(conn *websocket.Conn, channel string) {
	pubsub := api.subscriber.PSubscribe(context.Background(), channel)
	defer pubsub.Close()

	ch := pubsub.Channel()
	for msg := range ch {
		var logMsg LogMessage
		if err := json.Unmarshal([]byte(msg.Payload), &logMsg); err != nil {
			log.Printf("Failed to unmarshal log message: %v", err)
			continue
		}

		err := conn.WriteJSON(map[string]string{
			"message": msg.Payload,
		})
		if err != nil {
			log.Printf("WebSocket write error: %v", err)
			break
		}
	}
}

func (api *APIServer) initRedisSubscribe() {
	log.Println("Subscribed to logs....")
	
	pubsub := api.subscriber.PSubscribe(context.Background(), "logs:*")
	defer pubsub.Close()

	// This will be handled per WebSocket connection
	// The actual subscription forwarding happens in subscribeAndForward
}

func setupRoutes(api *APIServer) *gin.Engine {
	r := gin.Default()

	// Enable CORS
	r.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	})

	r.POST("/project", api.createProject)
	r.GET("/ws", api.handleWebSocket)

	return r
}

func main() {
	apiServer, err := NewAPIServer()
	if err != nil {
		log.Fatalf("Failed to initialize API server: %v", err)
	}

	// Initialize Redis subscription
	go apiServer.initRedisSubscribe()

	// Setup routes
	router := setupRoutes(apiServer)

	// Start server
	port := os.Getenv("PORT")
	if port == "" {
		port = "9000"
	}

	log.Printf("API Server running on port %s", port)
	if err := router.Run(":" + port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}