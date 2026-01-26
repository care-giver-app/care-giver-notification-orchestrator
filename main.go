package main

import (
	"context"
	"encoding/json"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/care-giver-app/care-giver-golang-common/pkg/awsconfig"
	"github.com/care-giver-app/care-giver-golang-common/pkg/dynamo"
	"github.com/care-giver-app/care-giver-golang-common/pkg/relationship"
	"github.com/care-giver-app/care-giver-golang-common/pkg/repository"
	"github.com/care-giver-app/care-giver-notification-orchestrator/internal/appconfig"
	"github.com/care-giver-app/care-giver-notification-orchestrator/internal/sqs"
	"go.uber.org/zap"
)

const (
	functionName = "care-giver-notification-orchestrator"
)

var (
	dynamoClient     *dynamodb.Client
	sqsClient        *awssqs.Client
	appCfg           *appconfig.AppConfig
	relationshipRepo *repository.RelationshipRepository
)

type Notification struct {
	NotificationType string   `json:"notification_type"`
	Channel          []string `json:"channel"`
	ExecutionData    any      `json:"execution_data"`
}

type ReminderNotification struct {
	Relationship relationship.Relationship `json:"relationship"`
}

func init() {
	appCfg = appconfig.NewAppConfig()
	appCfg.Logger.Sugar().Infof("initializing %s", functionName)

	cfg, err := awsconfig.GetAWSConfig(context.TODO(), appCfg.Env)
	if err != nil {
		appCfg.Logger.Sugar().Fatalf("Unable to load SDK config: %v", err)
	}

	appCfg.AWSConfig = cfg

	dynamoClient = dynamo.CreateClient(appCfg.Env, appCfg.AWSConfig, appCfg.Logger)
	sqsClient = sqs.CreateClient(appCfg)

	appCfg.Logger.Info("initializing relationship repository")
	relationshipRepo = repository.NewRelationshipRepository(context.TODO(), appCfg.RelationshipTableName, dynamoClient, appCfg.Logger)
}

func handler(ctx context.Context, event events.CloudWatchEvent) error {
	appCfg.Logger.Sugar().Infof("received scheduled event: %s", event.Source)
	appCfg.Logger.Sugar().Infof("event detail type: %s", event.DetailType)
	appCfg.Logger.Sugar().Infof("event time: %s", event.Time.String())

	if len(event.Detail) > 0 {
		appCfg.Logger.Sugar().Infof("event details: %s", string(event.Detail))
	}

	relationships, err := relationshipRepo.GetRelationshipsByEmailNotifications()
	if err != nil {
		appCfg.Logger.Error("error retrieving relationships with email notifications enabled", zap.Error(err))
		return err
	}

	appCfg.Logger.Sugar().Infof("retrieved relationships: %v", relationships)

	for _, r := range relationships {
		notificationMessage := Notification{
			ExecutionData: ReminderNotification{
				Relationship: r,
			},
			NotificationType: "reminder",
			Channel:          []string{"email"},
		}

		notificationMessageJson, err := json.Marshal(notificationMessage)
		if err != nil {
			appCfg.Logger.Error("error marshaling notification message to JSON", zap.Error(err))
			return err
		}

		_, err = sqsClient.SendMessage(ctx, &awssqs.SendMessageInput{
			QueueUrl:    &appCfg.SQSQueueURL,
			MessageBody: aws.String(string(notificationMessageJson)),
		})
		if err != nil {
			appCfg.Logger.Error("error sending message to SQS queue", zap.Error(err))
			return err
		}
	}

	appCfg.Logger.Info("notification processing completed successfully")
	return nil
}

func main() {
	lambda.Start(handler)
}
