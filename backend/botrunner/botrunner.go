// Package botrunner starts the bot runners that play hosted and uploaded
// bots' turns (#225), for services.TurnSender. Lambda invokes the runner
// functions of #223 asynchronously, as deployed. Local runs the same runners
// in this process, for local development without AWS: the native runner's
// Go code (backend/botrunner/native) and the Python runner's runner.py
// (backend/botrunner/python) in a child process. Neither ever waits for a
// bot: Invoke returns once the turn is queued, and the bot submits its own
// answer with the turn's submit token.
package botrunner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"islandBattles/backend/models"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

// LambdaAPI is the part of the Lambda client that Lambda uses; tests fake it.
type LambdaAPI interface {
	Invoke(ctx context.Context, params *lambda.InvokeInput, optFns ...func(*lambda.Options)) (*lambda.InvokeOutput, error)
}

// Lambda invokes the runner functions, by runner, asynchronously.
type Lambda struct {
	Client    LambdaAPI
	Functions map[models.BotRunnerName]string
}

// LambdaFromEnvironment invokes the functions python and native with the
// credentials and region of the environment, such as the backend
// function's role and AWS_REGION.
func LambdaFromEnvironment(ctx context.Context, python, native string) (*Lambda, error) {
	config, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load the AWS configuration for the bot runners: %w", err)
	}
	if config.Region == "" {
		return nil, errors.New("the bot runners need a region: set AWS_REGION")
	}
	return &Lambda{Client: lambda.NewFromConfig(config), Functions: map[models.BotRunnerName]string{
		models.BotRunnerPython: python, models.BotRunnerNative: native}}, nil
}

// Invoke queues event for the runner's function, with InvocationType Event:
// Lambda answers 202 once the event is queued, and the function then runs
// on its own. The function's asynchronous settings never retry it.
func (runners *Lambda) Invoke(ctx context.Context, runner models.BotRunnerName, event models.BotRunnerEvent) error {
	function := runners.Functions[runner]
	if function == "" {
		return fmt.Errorf("no function is configured for the %s bot runner", runner)
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	output, err := runners.Client.Invoke(ctx, &lambda.InvokeInput{
		FunctionName: aws.String(function), InvocationType: types.InvocationTypeEvent, Payload: payload})
	if err != nil {
		return fmt.Errorf("invoke %s: %w", function, err)
	}
	if output.StatusCode != http.StatusAccepted {
		return fmt.Errorf("invoke %s: Lambda answered %d, not 202", function, output.StatusCode)
	}
	return nil
}
