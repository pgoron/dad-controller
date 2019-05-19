package main

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

type TestContext struct {
	t                *testing.T
	controller       *dadController
	currentTime      time.Time
	runningProcesses []runningProcess
	killedProcesses  []string
}

func NewTest(t *testing.T) *TestContext {
	return &TestContext{t: t, currentTime: time.Now()}
}

func (ctx *TestContext) GivenADadControllerWithSamplingInterval(samplingInterval time.Duration) *TestContext {
	getTimeFunc := func() time.Time { return ctx.currentTime }
	ctx.controller = newDadController(samplingInterval, getTimeFunc)
	ctx.controller.GetTime = getTimeFunc
	ctx.controller.KillRunningProcesses = func(activity string, rp []runningProcess, reason string) {
		for _, p := range rp {
			ctx.killedProcesses = append(ctx.killedProcesses, fmt.Sprintf("%s|%d|%s|%s", activity, p.Pid, p.Path, reason))
		}
	}
	return ctx
}

func (ctx *TestContext) GivenAnActivityRuleAllowedEveryTime(activity string, program string, allowedDuration time.Duration) *TestContext {
	ar := ctx.controller.getOrCreateActivityRule(activity)
	ar.AddProgramPattern(program)
	everyDays := []time.Weekday{time.Sunday, time.Monday, time.Tuesday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday}
	ar.SetMaximumAllowedDurationPerDay(everyDays, allowedDuration)
	ar.AddAllowedPeriod(everyDays, 0, 2359)
	return ctx
}

func (ctx *TestContext) GivenAnActivityRuleAllowedEveryDayOnInterval(activity string, program string, allowedDuration time.Duration, begin int, end int) *TestContext {
	ar := ctx.controller.getOrCreateActivityRule(activity)
	ar.AddProgramPattern(program)
	everyDays := []time.Weekday{time.Sunday, time.Monday, time.Tuesday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday}
	ar.SetMaximumAllowedDurationPerDay(everyDays, allowedDuration)
	ar.AddAllowedPeriod(everyDays, begin, end)
	return ctx
}

func (ctx *TestContext) GivenAnActivityRuleAllowedOnlyOnSunday(activity string, program string, allowedDuration time.Duration) *TestContext {
	ar := ctx.controller.getOrCreateActivityRule(activity)
	ar.AddProgramPattern(program)
	sunday := []time.Weekday{time.Sunday}
	ar.SetMaximumAllowedDurationPerDay(sunday, allowedDuration)
	ar.AddAllowedPeriod(sunday, 0, 2359)
	return ctx
}

func (ctx *TestContext) GivenAnActivityDuration(activity string, duration time.Duration) *TestContext {
	ctx.controller.updateActivityDuration(activity, duration)
	return ctx
}

func (ctx *TestContext) GivenARunningProcess(path string, pid int) *TestContext {
	ctx.runningProcesses = append(ctx.runningProcesses, runningProcess{Path: path, Pid: pid})
	ctx.controller.GetRunningProcesses = func() []runningProcess { return ctx.runningProcesses }
	return ctx
}

func (ctx *TestContext) WhenDayChanges() *TestContext {
	rp := make(map[string][]runningProcess)
	ctx.controller.updateActivityCounters(rp, ctx.controller.LastControlTime.Add(time.Duration(24)*time.Hour))
	return ctx
}

func (ctx *TestContext) WhenScanHappens() *TestContext {
	ctx.killedProcesses = []string{}
	ctx.currentTime = ctx.currentTime.Add(time.Duration(ctx.controller.SamplingInterval))
	ctx.controller.scan()
	return ctx
}

func (ctx *TestContext) ThenActivityExecutionDurationShouldBe(activity string, expectedDuration time.Duration) *TestContext {
	activityDuration := ctx.controller.GetActivityDuration(activity)
	if activityDuration != expectedDuration {
		ctx.t.Errorf("Activity %s execution duration is %s (expected %s)\n", activity, activityDuration, expectedDuration)
	}
	return ctx
}

func (ctx *TestContext) GivenTimeIs(t time.Time) *TestContext {
	ctx.currentTime = t
	return ctx
}

func (ctx *TestContext) ThenNoProcessKilled() *TestContext {
	if len(ctx.killedProcesses) > 0 {
		ctx.t.Error("Some processes have been killed")
	}
	return ctx
}

func (ctx *TestContext) ThenProcessIsKilled(activity string, pid int, path string, reason string) *TestContext {
	info := fmt.Sprintf("%s|%d|%s|%s", activity, pid, path, reason)
	found := false
	for _, k := range ctx.killedProcesses {
		if k == info {
			found = true
			break
		}
	}
	if !found {
		ctx.t.Errorf("%s not found in list of processes killed", info)
	}
	return ctx
}

func TestProcessAreProperlyMappedToActivity(t *testing.T) {
	NewTest(t).
		GivenADadControllerWithSamplingInterval(time.Duration(1)*time.Minute).
		GivenAnActivityRuleAllowedEveryTime("GTA", "GTA.exe", time.Duration(15)*time.Minute).
		GivenARunningProcess("C:\\GTA.exe", 1).
		WhenScanHappens().
		ThenActivityExecutionDurationShouldBe("GTA", time.Duration(1)*time.Minute)
}

func TestActivityCountersMustBeResettedWhenChangingDay(t *testing.T) {
	NewTest(t).
		GivenADadControllerWithSamplingInterval(time.Duration(1)*time.Minute).
		GivenAnActivityRuleAllowedEveryTime("GTA", "GTA.exe", time.Duration(15)*time.Minute).
		GivenAnActivityDuration("GTA", time.Duration(14)*time.Minute).
		ThenActivityExecutionDurationShouldBe("GTA", time.Duration(14)*time.Minute).
		WhenDayChanges().
		ThenActivityExecutionDurationShouldBe("GTA", time.Duration(0)*time.Minute)
}

func TestRunningProcessIsKilledIfRunningOnANonAllowedDay(t *testing.T) {
	notSunday := time.Now()
	if notSunday.Weekday() == time.Sunday {
		notSunday = notSunday.Add(time.Duration(24) * time.Hour)
	}
	NewTest(t).
		GivenADadControllerWithSamplingInterval(time.Duration(1)*time.Minute).
		GivenAnActivityRuleAllowedOnlyOnSunday("GTA", "GTA.exe", time.Duration(15)*time.Minute).
		GivenTimeIs(notSunday).
		GivenARunningProcess("C:\\GTA.exe", 1).
		WhenScanHappens().
		ThenActivityExecutionDurationShouldBe("GTA", time.Duration(1)*time.Minute).
		ThenProcessIsKilled("GTA", 1, "C:\\GTA.exe", "Activity not allowed to be done on this day")
}

func TestRunningProcessIsKilledIfRunningLongerThanAllowed(t *testing.T) {
	NewTest(t).
		GivenADadControllerWithSamplingInterval(time.Duration(1)*time.Minute).
		GivenAnActivityRuleAllowedEveryTime("GTA", "GTA.exe", time.Duration(15)*time.Minute).
		GivenAnActivityDuration("GTA", time.Duration(14)*time.Minute).
		GivenARunningProcess("C:\\GTA.exe", 1).
		WhenScanHappens().
		ThenActivityExecutionDurationShouldBe("GTA", time.Duration(15)*time.Minute).
		ThenNoProcessKilled().
		WhenScanHappens().
		ThenActivityExecutionDurationShouldBe("GTA", time.Duration(16)*time.Minute).
		ThenProcessIsKilled("GTA", 1, "C:\\GTA.exe", "Activity duration above threshold for this day")
}

func TestRunningProcessIsKilledIfRunningOutsideOfAllowedPeriods(t *testing.T) {
	now := time.Now()
	beforePeriod := time.Date(now.Year(), now.Month(), now.Day(), 18, 0, 0, 0, time.Local)
	afterPeriod := time.Date(now.Year(), now.Month(), now.Day(), 21, 0, 0, 0, time.Local)

	NewTest(t).
		GivenADadControllerWithSamplingInterval(time.Duration(1)*time.Minute).
		GivenAnActivityRuleAllowedEveryDayOnInterval("GTA", "GTA.exe", time.Duration(15)*time.Minute, 2000, 2100).
		GivenTimeIs(beforePeriod).
		GivenARunningProcess("C:\\GTA.exe", 1).
		WhenScanHappens().
		ThenActivityExecutionDurationShouldBe("GTA", time.Duration(1)*time.Minute).
		ThenProcessIsKilled("GTA", 1, "C:\\GTA.exe", "Activity not allowed to be done during this time range").
		GivenTimeIs(afterPeriod).
		WhenScanHappens().
		ThenActivityExecutionDurationShouldBe("GTA", time.Duration(2)*time.Minute).
		ThenProcessIsKilled("GTA", 1, "C:\\GTA.exe", "Activity not allowed to be done during this time range")
}

func TestJson(t *testing.T) {

	ctx := NewTest(t).
		GivenADadControllerWithSamplingInterval(time.Duration(1)*time.Minute).
		GivenAnActivityRuleAllowedEveryDayOnInterval("GTA", "GTA.exe", time.Duration(15)*time.Minute, 2000, 2100)

	data, _ := json.Marshal(ctx.controller.Activities)
	fmt.Println(string(data))
}
