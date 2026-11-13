// Package botcompute measures the compute that the bot runner functions
// (#223) use, for the monthly guard on games with uploaded bots (#226). It
// reads the runners' month-to-date use from CloudWatch, rather than
// estimating it from the turns sent: the AWS/Lambda Duration metric, and
// InitDuration where the account publishes it, summed over both functions
// since the first of the month (UTC), times their memory.
package botcompute

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
)

// RunnerMemoryGB is the bot runners' memory, 1024 MB
// (deploy/terraform/bot_runners.tf), by which a millisecond of their duration
// is billed.
const RunnerMemoryGB = 1.0

// period is the length of each datapoint that CloudWatch sums: an hour,
// which CloudWatch keeps for 455 days and which divides every month, so a
// month is at most 744 datapoints per metric.
const period = time.Hour

// metrics are the AWS/Lambda metrics that a runner is billed for, in
// milliseconds. InitDuration has no datapoints where the account doesn't
// publish it, and counts nothing then.
var metrics = []string{"Duration", "InitDuration"}

// MetricsAPI is the part of the CloudWatch client that CloudWatch uses; tests
// fake it.
type MetricsAPI interface {
	GetMetricData(ctx context.Context, params *cloudwatch.GetMetricDataInput, optFns ...func(*cloudwatch.Options)) (*cloudwatch.GetMetricDataOutput, error)
}

// CloudWatch reads the runner functions' month-to-date compute.
type CloudWatch struct {
	Client MetricsAPI
	// Functions are the runner functions' names.
	Functions []string
}

// CloudWatchFromEnvironment reads the functions' metrics with the credentials
// and region of the environment, such as the backend function's role, which
// needs cloudwatch:GetMetricData.
func CloudWatchFromEnvironment(ctx context.Context, functions ...string) (*CloudWatch, error) {
	config, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load the AWS configuration for the bot runners' metrics: %w", err)
	}
	if config.Region == "" {
		return nil, errors.New("the bot runners' metrics need a region: set AWS_REGION")
	}
	return &CloudWatch{Client: cloudwatch.NewFromConfig(config), Functions: functions}, nil
}

// MonthToDate returns the GB-seconds that the functions have used from
// month, the first of a month at midnight UTC, until now: the sum of their
// Duration and InitDuration, in hourly datapoints, times RunnerMemoryGB.
func (reader *CloudWatch) MonthToDate(ctx context.Context, month, now time.Time) (float64, error) {
	if len(reader.Functions) == 0 {
		return 0, errors.New("no bot runner functions to measure")
	}
	queries := make([]types.MetricDataQuery, 0, len(reader.Functions)*len(metrics))
	for functionIndex, function := range reader.Functions {
		for metricIndex, metric := range metrics {
			queries = append(queries, types.MetricDataQuery{
				Id: aws.String(fmt.Sprintf("m%d_%d", functionIndex, metricIndex)),
				MetricStat: &types.MetricStat{
					Metric: &types.Metric{
						Namespace:  aws.String("AWS/Lambda"),
						MetricName: aws.String(metric),
						Dimensions: []types.Dimension{{Name: aws.String("FunctionName"), Value: aws.String(function)}},
					},
					Period: aws.Int32(int32(period / time.Second)),
					Stat:   aws.String("Sum"),
				},
				ReturnData: aws.Bool(true),
			})
		}
	}
	// The end is the next whole hour, so the current hour counts.
	input := &cloudwatch.GetMetricDataInput{
		MetricDataQueries: queries,
		StartTime:         aws.Time(month.UTC()),
		EndTime:           aws.Time(now.UTC().Truncate(period).Add(period)),
		ScanBy:            types.ScanByTimestampAscending,
	}
	var milliseconds float64
	for {
		output, err := reader.Client.GetMetricData(ctx, input)
		if err != nil {
			return 0, fmt.Errorf("read the bot runners' metrics: %w", err)
		}
		for _, result := range output.MetricDataResults {
			if result.StatusCode == types.StatusCodeForbidden || result.StatusCode == types.StatusCodeInternalError {
				return 0, fmt.Errorf("read the bot runners' metrics: %s: %s", aws.ToString(result.Id), result.StatusCode)
			}
			for _, value := range result.Values {
				milliseconds += value
			}
		}
		if aws.ToString(output.NextToken) == "" {
			break
		}
		input.NextToken = output.NextToken
	}
	return milliseconds / 1000 * RunnerMemoryGB, nil
}
