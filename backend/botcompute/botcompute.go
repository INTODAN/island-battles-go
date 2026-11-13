// Package botcompute measures the compute that the bot runner functions
// (#223) use, for the monthly guard on games with uploaded bots (#226). It
// reads the runners' month-to-date use from CloudWatch, rather than
// estimating it from the turns sent: the AWS/Lambda Duration metric, and
// InitDuration where the account publishes it, summed over both functions
// since the first of the month (UTC), times their memory.
//
// It reads them with GetMetricStatistics, whose requests count against
// CloudWatch's free 1,000,000 API requests a month, rather than
// GetMetricData, which is billed from the first metric.
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

// period is the length of each datapoint that CloudWatch sums: an hour. A
// month is at most 744 of them, under GetMetricStatistics' 1,440 datapoints
// per call. CloudWatch keeps hourly data for 455 days, and the first of a
// month at midnight is aligned to the hour, as a start more than 63 days ago
// must be.
const period = time.Hour

// metrics are the AWS/Lambda metrics that a runner is billed for, in
// milliseconds. InitDuration has no datapoints where the account doesn't
// publish it, and counts nothing then.
var metrics = []string{"Duration", "InitDuration"}

// MetricsAPI is the part of the CloudWatch client that CloudWatch uses; tests
// fake it.
type MetricsAPI interface {
	GetMetricStatistics(ctx context.Context, params *cloudwatch.GetMetricStatisticsInput, optFns ...func(*cloudwatch.Options)) (*cloudwatch.GetMetricStatisticsOutput, error)
}

// CloudWatch reads the runner functions' month-to-date compute.
type CloudWatch struct {
	Client MetricsAPI
	// Functions are the runner functions' names.
	Functions []string
}

// CloudWatchFromEnvironment reads the functions' metrics with the credentials
// and region of the environment, such as the backend function's role, which
// needs cloudwatch:GetMetricStatistics.
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
// Duration and InitDuration, in hourly datapoints, times RunnerMemoryGB. It
// makes one GetMetricStatistics call per function and metric.
func (reader *CloudWatch) MonthToDate(ctx context.Context, month, now time.Time) (float64, error) {
	if len(reader.Functions) == 0 {
		return 0, errors.New("no bot runner functions to measure")
	}
	// The end is the next whole hour, so the current hour counts.
	start, end := month.UTC(), now.UTC().Truncate(period).Add(period)
	var milliseconds float64
	for _, function := range reader.Functions {
		for _, metric := range metrics {
			output, err := reader.Client.GetMetricStatistics(ctx, &cloudwatch.GetMetricStatisticsInput{
				Namespace:  aws.String("AWS/Lambda"),
				MetricName: aws.String(metric),
				Dimensions: []types.Dimension{{Name: aws.String("FunctionName"), Value: aws.String(function)}},
				StartTime:  aws.Time(start),
				EndTime:    aws.Time(end),
				Period:     aws.Int32(int32(period / time.Second)),
				Statistics: []types.Statistic{types.StatisticSum},
			})
			if err != nil {
				return 0, fmt.Errorf("read %s of %s: %w", metric, function, err)
			}
			for _, datapoint := range output.Datapoints {
				milliseconds += aws.ToFloat64(datapoint.Sum)
			}
		}
	}
	return milliseconds / 1000 * RunnerMemoryGB, nil
}
