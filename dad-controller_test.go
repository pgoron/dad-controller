package main

import (
	"testing"
	"time"
)

type TestContext struct {
	t                *testing.T
	controller       *dadController
	runningProcesses []runningProcess
}

func NewTest(t *testing.T) *TestContext {
	return &TestContext{t: t}
}

func (ctx *TestContext) GivenADadControllerWithSamplingInterval(samplingInterval time.Duration) *TestContext {
	ctx.controller = newDadController(samplingInterval)
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
	rp := make(map[string][]*runningProcess)
	ctx.controller.updateActivityCounters(rp, ctx.controller.LastControlTime.Add(time.Duration(24)*time.Hour))
	return ctx
}

func (ctx *TestContext) WhenScanHappens() *TestContext {
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

func (ctx *TestContext) ThenNoProcessKilled() *TestContext {
	ctx.t.Error("ThenNoProcessKilled Not implemented")
	return ctx
}

func (ctx *TestContext) ThenProcessIsKilled(activity string, expectedDuration time.Duration) *TestContext {
	ctx.t.Error("ThenProcessIsKilled Not implemented")
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
	NewTest(t).
		GivenADadControllerWithSamplingInterval(time.Duration(1)*time.Minute).
		GivenAnActivityRuleAllowedOnlyOnSunday("GTA", "GTA.exe", time.Duration(15)*time.Minute).
		GivenARunningProcess("C:\\GTA.exe", 1).
		WhenScanHappens().
		ThenActivityExecutionDurationShouldBe("GTA", time.Duration(1)*time.Minute).
		ThenProcessIsKilled("C:\\GTA.exe", 1)
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
		ThenProcessIsKilled("C:\\GTA.exe", 1)
}
func TestRunningProcessIsKilledIfRunningOutsideOfAllowedPeriods(t *testing.T) {
	NewTest(t).
		GivenADadControllerWithSamplingInterval(time.Duration(1)*time.Minute).
		GivenAnActivityRuleAllowedEveryDayOnInterval("GTA", "GTA.exe", time.Duration(15)*time.Minute, 2000, 2100).
		GivenARunningProcess("C:\\GTA.exe", 1).
		WhenScanHappens().
		ThenActivityExecutionDurationShouldBe("GTA", time.Duration(1)*time.Minute).
		ThenProcessIsKilled("C:\\GTA.exe", 1)
}
