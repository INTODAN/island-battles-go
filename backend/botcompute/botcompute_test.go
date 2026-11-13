package botcompute_test

import (
	"context"
	"errors"
	"math"
	"slices"
	"testing"
	"time"

	"islandBattles/backend/botcompute"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
)

// fakeMetrics answers GetMetricStatistics with hourly sums by function and
// metric, and keeps the requests.
type fakeMetrics struct {
	sums     map[string][]float64
	err      error
	requests []cloudwatch.GetMetricStatisticsInput
}

func (metrics *fakeMetrics) GetMetricStatistics(_ context.Context, input *cloudwatch.GetMetricStatisticsInput, _ ...func(*cloudwatch.Options)) (*cloudwatch.GetMetricStatisticsOutput, error) {
	metrics.requests = append(metrics.requests, *input)
	if metrics.err != nil {
		return nil, metrics.err
	}
	output := &cloudwatch.GetMetricStatisticsOutput{Label: input.MetricName}
	for _, sum := range metrics.sums[aws.ToString(input.Dimensions[0].Value)+" "+aws.ToString(input.MetricName)] {
		output.Datapoints = append(output.Datapoints, types.Datapoint{Sum: aws.Float64(sum), Unit: types.StandardUnitMilliseconds})
	}
	return output, nil
}

var (
	november = time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC)
	now      = time.Date(2026, time.November, 13, 10, 41, 7, 0, time.UTC)
)

// Duration and InitDuration of both runners, summed over the month's hourly
// datapoints, are milliseconds of 1 GB, read with one call per function and
// metric.
func TestMonthToDateSumsBothRunnersDurationAndInitDuration(t *testing.T) {
	metrics := &fakeMetrics{sums: map[string][]float64{
		"island-battles-bot-runner-python Duration":     {1_000_000, 500_000},
		"island-battles-bot-runner-python InitDuration": {2_000},
		"island-battles-bot-runner-native Duration":     {250_000},
	}}
	reader := &botcompute.CloudWatch{Client: metrics, Functions: []string{"island-battles-bot-runner-python", "island-battles-bot-runner-native"}}
	used, err := reader.MonthToDate(context.Background(), november, now)
	if err != nil {
		t.Fatal(err)
	}
	if want := 1752.0; math.Abs(used-want) > 1e-9 {
		t.Fatalf("used %v GB-s, want %v", used, want)
	}
	var asked []string
	for _, request := range metrics.requests {
		if aws.ToString(request.Namespace) != "AWS/Lambda" || !slices.Equal(request.Statistics, []types.Statistic{types.StatisticSum}) || aws.ToInt32(request.Period) != 3600 {
			t.Fatalf("request %+v", request)
		}
		if !aws.ToTime(request.StartTime).Equal(november) || !aws.ToTime(request.EndTime).Equal(time.Date(2026, time.November, 13, 11, 0, 0, 0, time.UTC)) {
			t.Fatalf("from %v to %v, want the first of the month to the end of the current hour", request.StartTime, request.EndTime)
		}
		// Hourly datapoints of a whole month fit one call.
		if hours := aws.ToTime(request.EndTime).Sub(aws.ToTime(request.StartTime)) / time.Hour; hours > 1440 {
			t.Fatalf("%d datapoints in one call", hours)
		}
		dimension := request.Dimensions[0]
		asked = append(asked, aws.ToString(dimension.Name)+"="+aws.ToString(dimension.Value)+" "+aws.ToString(request.MetricName))
	}
	want := []string{
		"FunctionName=island-battles-bot-runner-python Duration", "FunctionName=island-battles-bot-runner-python InitDuration",
		"FunctionName=island-battles-bot-runner-native Duration", "FunctionName=island-battles-bot-runner-native InitDuration",
	}
	if !slices.Equal(asked, want) {
		t.Fatalf("asked for %v, want %v", asked, want)
	}
}

func TestMonthToDateFailsWhenCloudWatchDoes(t *testing.T) {
	failing := &botcompute.CloudWatch{Client: &fakeMetrics{err: errors.New("AccessDenied")}, Functions: []string{"runner"}}
	if _, err := failing.MonthToDate(context.Background(), november, now); err == nil {
		t.Fatal("a failed request read")
	}
	none := &botcompute.CloudWatch{Client: &fakeMetrics{}}
	if _, err := none.MonthToDate(context.Background(), november, now); err == nil {
		t.Fatal("no functions read as 0")
	}
}
