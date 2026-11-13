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

// fakeMetrics answers GetMetricData with pages, in order, and keeps the
// requests.
type fakeMetrics struct {
	pages    []*cloudwatch.GetMetricDataOutput
	err      error
	requests []cloudwatch.GetMetricDataInput
}

func (metrics *fakeMetrics) GetMetricData(_ context.Context, input *cloudwatch.GetMetricDataInput, _ ...func(*cloudwatch.Options)) (*cloudwatch.GetMetricDataOutput, error) {
	metrics.requests = append(metrics.requests, *input)
	if metrics.err != nil {
		return nil, metrics.err
	}
	page := metrics.pages[0]
	metrics.pages = metrics.pages[1:]
	return page, nil
}

func result(id string, values ...float64) types.MetricDataResult {
	return types.MetricDataResult{Id: aws.String(id), Values: values, StatusCode: types.StatusCodeComplete}
}

var (
	november = time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC)
	now      = time.Date(2026, time.November, 13, 10, 41, 7, 0, time.UTC)
)

// Duration and InitDuration of both runners, summed over the month's hourly
// datapoints and every page, are milliseconds of 1 GB.
func TestMonthToDateSumsBothRunnersDurationAndInitDuration(t *testing.T) {
	metrics := &fakeMetrics{pages: []*cloudwatch.GetMetricDataOutput{
		{MetricDataResults: []types.MetricDataResult{result("m0_0", 1_000_000, 500_000), result("m0_1", 2_000)}, NextToken: aws.String("more")},
		{MetricDataResults: []types.MetricDataResult{result("m1_0", 250_000), result("m1_1")}},
	}}
	reader := &botcompute.CloudWatch{Client: metrics, Functions: []string{"island-battles-bot-runner-python", "island-battles-bot-runner-native"}}
	used, err := reader.MonthToDate(context.Background(), november, now)
	if err != nil {
		t.Fatal(err)
	}
	if want := 1752.0; math.Abs(used-want) > 1e-9 {
		t.Fatalf("used %v GB-s, want %v", used, want)
	}
	if len(metrics.requests) != 2 || aws.ToString(metrics.requests[1].NextToken) != "more" {
		t.Fatalf("requests %+v, want a second page with the token", metrics.requests)
	}
	request := metrics.requests[0]
	if !aws.ToTime(request.StartTime).Equal(november) || !aws.ToTime(request.EndTime).Equal(time.Date(2026, time.November, 13, 11, 0, 0, 0, time.UTC)) {
		t.Fatalf("from %v to %v, want the first of the month to the end of the current hour", request.StartTime, request.EndTime)
	}
	var asked []string
	for _, query := range request.MetricDataQueries {
		stat := query.MetricStat
		if aws.ToString(stat.Metric.Namespace) != "AWS/Lambda" || aws.ToString(stat.Stat) != "Sum" || aws.ToInt32(stat.Period) != 3600 || !aws.ToBool(query.ReturnData) {
			t.Fatalf("query %s: %+v", aws.ToString(query.Id), stat)
		}
		dimension := stat.Metric.Dimensions[0]
		asked = append(asked, aws.ToString(dimension.Name)+"="+aws.ToString(dimension.Value)+" "+aws.ToString(stat.Metric.MetricName))
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
	forbidden := &botcompute.CloudWatch{Client: &fakeMetrics{pages: []*cloudwatch.GetMetricDataOutput{{MetricDataResults: []types.MetricDataResult{
		{Id: aws.String("m0_0"), StatusCode: types.StatusCodeForbidden},
	}}}}, Functions: []string{"runner"}}
	if _, err := forbidden.MonthToDate(context.Background(), november, now); err == nil {
		t.Fatal("a forbidden metric read as 0")
	}
	none := &botcompute.CloudWatch{Client: &fakeMetrics{}}
	if _, err := none.MonthToDate(context.Background(), november, now); err == nil {
		t.Fatal("no functions read as 0")
	}
}
