package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"regexp"
	"time"
)

type timePeriod struct {
	Begin int `json:"begin"`
	End   int `json:"end"`
}

type schedule struct {
	AllowedPeriods []timePeriod  `json:"allowedPeriods"`
	MaxDuration    time.Duration `json:"maxDuration"`
}

type activityRule struct {
	Name             string                     `json:"name"`
	ProcessPatterns  []string                   `json:"programs"`
	AllowedSchedules map[time.Weekday]*schedule `json:"schedules"`
}

type dadController struct {
	// configuration
	configFile      string
	confLastModTime time.Time

	SamplingInterval time.Duration   `json:"samplingInterval"`
	Activities       []*activityRule `json:"rules"`

	// hook for tests
	GetTime              func() time.Time
	GetRunningProcesses  func() []runningProcess
	KillRunningProcesses func(activity string, rp []*runningProcess, reason string)
	WarnAboutKill        func(activity string, rp []*runningProcess, reason string)

	// state
	LastControlTime  time.Time
	ActivityDuration map[time.Weekday]map[string]time.Duration
}

type runningProcess struct {
	Pid  int    `json:"Id"`
	Path string `json:"Path"`
}

// NewDadController returns a new instance of IDadController
func newDadController(samplingInterval time.Duration, getTimeFunc func() time.Time) *dadController {
	return &dadController{SamplingInterval: samplingInterval,
		ActivityDuration:     make(map[time.Weekday]map[string]time.Duration),
		GetTime:              getTimeFunc,
		GetRunningProcesses:  getRunningProcesses,
		KillRunningProcesses: kill,
		WarnAboutKill:        warn,
		LastControlTime:      getTimeFunc(),
	}
}

func newDadControllerWithConfigFile(configFile string) *dadController {
	getTimeFunc := time.Now
	ctrl := &dadController{
		configFile:           configFile,
		ActivityDuration:     make(map[time.Weekday]map[string]time.Duration),
		GetTime:              getTimeFunc,
		GetRunningProcesses:  getRunningProcesses,
		KillRunningProcesses: kill,
		WarnAboutKill:        warn,
		LastControlTime:      getTimeFunc(),
	}
	ctrl.reloadConfIfNeeded()
	return ctrl
}

func (c *dadController) reloadConfIfNeeded() {
	stat, err := os.Stat(c.configFile)
	if err != nil {
		panic(err)
	}
	if stat.ModTime().After(c.confLastModTime) {
		fmt.Println("Detecting change of configuration. Reloading it.")
		c.confLastModTime = stat.ModTime()

		jsonFile, err := os.Open(c.configFile)
		if err != nil {
			panic(err)
		}
		defer jsonFile.Close()

		data, err := ioutil.ReadAll(jsonFile)
		if err != nil {
			panic(err)
		}

		var tmpCtrl dadController
		json.Unmarshal(data, &tmpCtrl)

		c.Activities = tmpCtrl.Activities
		c.SamplingInterval = tmpCtrl.SamplingInterval

		fmt.Printf("Sampling Interval: %s\n", c.SamplingInterval)
		for idx := range c.Activities {
			fmt.Printf("Activity [%s]\n", c.Activities[idx].Name)

		}
	}
}

func (c *dadController) GetActivityDuration(activity string) time.Duration {
	day := c.LastControlTime.Weekday()
	ad, found := c.ActivityDuration[day]
	if !found {
		return time.Duration(0)
	}

	d, found := ad[activity]
	if !found {
		return time.Duration(0)
	}

	return d
}

func (c *dadController) updateActivityDuration(activity string, duration time.Duration) {
	day := c.LastControlTime.Weekday()

	// make activity duration for the current day available
	ad, found := c.ActivityDuration[day]
	if !found {
		ad = make(map[string]time.Duration)
		c.ActivityDuration[day] = ad
	}

	ad[activity] = duration
}

func (c *dadController) getOrCreateActivityRule(activity string) *activityRule {
	for _, a := range c.Activities {
		if a.Name == activity {
			return a
		}
	}

	a := activityRule{Name: activity, AllowedSchedules: make(map[time.Weekday]*schedule)}
	c.Activities = append(c.Activities, &a)
	return &a
}

func (a *activityRule) AddProgramPattern(programPattern string) {
	a.ProcessPatterns = append(a.ProcessPatterns, programPattern)
}

func (a *activityRule) getOrCreateSchedule(day time.Weekday) *schedule {
	s, found := a.AllowedSchedules[day]
	if !found {
		s = &schedule{}
		a.AllowedSchedules[day] = s
	}

	return s
}

func (a *activityRule) AddAllowedPeriod(days []time.Weekday, begin int, end int) {
	for _, d := range days {
		s := a.getOrCreateSchedule(d)
		s.AllowedPeriods = append(s.AllowedPeriods, timePeriod{Begin: begin, End: end})
	}
}

func (a *activityRule) SetMaximumAllowedDurationPerDay(days []time.Weekday, maximumAllowedDurationPerDay time.Duration) {
	for _, d := range days {
		a.getOrCreateSchedule(d).MaxDuration = maximumAllowedDurationPerDay
	}
}

func (c *dadController) scan() {
	rp := c.getRunningProcessesPerActivity()
	c.updateActivityCounters(rp, c.GetTime())
	c.controlActivities(rp)
}

func (c *dadController) getRunningProcessesPerActivity() map[string][]*runningProcess {
	processes := c.GetRunningProcesses()

	// map processes to activities
	results := make(map[string][]*runningProcess)
	for _, activity := range c.Activities {
		for _, processPattern := range activity.ProcessPatterns {
			r, _ := regexp.Compile(processPattern)

			for _, rp := range processes {
				if r.MatchString(rp.Path) {
					r, found := results[activity.Name]
					if !found {
						r = []*runningProcess{}
						results[activity.Name] = r
					}
					results[activity.Name] = append(r, &rp)
				}
			}
		}
	}

	return results
}

func (c *dadController) updateActivityCounters(rp map[string][]*runningProcess, now time.Time) {
	if now.Year() != c.LastControlTime.Year() ||
		now.Month() != c.LastControlTime.Month() ||
		now.Day() != c.LastControlTime.Day() {
		// change of day detected, reset of counters
		delete(c.ActivityDuration, now.Weekday())
	}
	c.LastControlTime = now

	if len(rp) > 0 {
		day := c.LastControlTime.Weekday()

		// make activity duration for the current day is available
		ad, found := c.ActivityDuration[day]
		if !found {
			ad = make(map[string]time.Duration)
			c.ActivityDuration[day] = ad
		}

		// update duration counters
		for activity := range rp {
			d, found := ad[activity]
			if !found {
				d = time.Duration(0)
			}
			ad[activity] = d + c.SamplingInterval
		}
	}

}

func (c *dadController) controlActivities(rp map[string][]*runningProcess) {
	day := c.LastControlTime.Weekday()
	time := c.LastControlTime.Hour()*100 + c.LastControlTime.Minute()

	ad, found := c.ActivityDuration[day]
	if !found {
		// should never happen
		return
	}

	for activity := range rp {
		a := c.getOrCreateActivityRule(activity)

		schedule, found := a.AllowedSchedules[day]
		if !found {
			fmt.Printf("/!\\ %s activity not allowed to run on %s\n", activity, day.String())
			c.KillRunningProcesses(activity, rp[activity], "Activity not allowed to be done on this day")
			continue
		}

		if ad[activity] > schedule.MaxDuration {
			fmt.Printf("/!\\ %s activity is above max duration %s for %s (currently %s)\n", activity, schedule.MaxDuration.String(), day.String(), ad[activity])
			c.KillRunningProcesses(activity, rp[activity], "Activity duration above threshold for this day")
			continue
		}

		// TODO warning duration

		foundValidPeriod := false
		for _, ap := range schedule.AllowedPeriods {
			if time >= ap.Begin && time < ap.End {
				foundValidPeriod = true
			}
		}

		if !foundValidPeriod {
			fmt.Printf("/!\\ %s activity is not allowed to run at this time\n", activity)
			c.KillRunningProcesses(activity, rp[activity], "Activity not allowed to be done during this time range")
			continue
		}
	}
}

func getRunningProcesses() []runningProcess {
	cmd := exec.Command("powershell", "-Command", "& { ps | Select-Object Id,Path | ?{$_.Path -ne $null} | convertto-json }")

	cmdOut, err := cmd.StdoutPipe()
	if err != nil {
		panic(err)
	}

	err = cmd.Start()
	if err != nil {
		panic(err)
	}

	data, err := ioutil.ReadAll(cmdOut)
	if err != nil {
		panic(err)
	}

	var processes []runningProcess
	if err := json.Unmarshal(data, &processes); err != nil {
		panic(err)
	}

	return processes
}

func warn(activity string, rp []*runningProcess, reason string) {

}

func kill(activity string, rp []*runningProcess, reason string) {

}

func main() {
	ctrl := newDadControllerWithConfigFile("dad-controller.json")

	for {
		ctrl.reloadConfIfNeeded()
		time.Sleep(ctrl.SamplingInterval)
		ctrl.scan()
	}
}
