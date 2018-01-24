package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// data types for parsing and processing input data

type Instructor struct {
	Name     string
	Times    map[*Time]Badness
	Courses  []*Course
	Days     int
	MinRooms int
}

type Course struct {
	Name       string
	Instructor *Instructor
	Rooms      map[*Room]Badness
	Times      map[*Time]Badness
	Conflicts  map[*Course]Badness
	Slots      int
	PinRoom    *Room
	PinTime    *Time
}

type Room struct {
	Name     string
	Tags     []string
	Position int
}

type Time struct {
	Name     string
	Tags     []string
	Next     *Time
	Position int
}

func (t *Time) Prefix() string {
	brk := strings.IndexAny(t.Name, "0123456789")
	if brk < 0 {
		return ""
	}
	return t.Name[:brk]
}

type Badness struct {
	N       int
	Message string
}

var impossible = Badness{N: -1, Message: ""}

type Conflict struct {
	Badness Badness
	Courses []*Course
}

type DataSet struct {
	Instructors map[string]*Instructor
	Rooms       map[string]*Room
	Times       map[string]*Time
	TagToRooms  map[string][]*Room
	TagToTimes  map[string][]*Time
	Conflicts   []Conflict
}

// data types to represent a search in progress
type RoomTimeBadness struct {
	Room    *Room
	Time    *Time
	Badness Badness
}

type Section struct {
	Instructor      *Instructor
	Course          *Course
	RoomTimeOptions []RoomTimeBadness
}

type InstructorTime struct {
	Instructor *Instructor
	Time       *Time
}

type CourseTime struct {
	Course *Course
	Time   *Time
}

type RoomTime struct {
	Room *Room
	Time *Time
}

type SearchState struct {
	Data                  *DataSet
	Sections              []*Section
	InstructorTimeBadness map[InstructorTime]Badness
	CourseTimeBadness     map[CourseTime]Badness
	RoomTimeBadness       map[RoomTime]Badness
	PinMean               float64
	PinStddev             float64
	ReSort                int
	Badness               int
	Schedule              []*CoursePlacement
	Generation            int
	BadNotes              []string
}

type CoursePlacement struct {
	Course  *Course
	Room    *Room
	Time    *Time
	Badness Badness
}

func NewSearchState(data *DataSet, pin, pinDev float64, resort int) *SearchState {
	state := &SearchState{
		Data: data,
		InstructorTimeBadness: make(map[InstructorTime]Badness),
		CourseTimeBadness:     make(map[CourseTime]Badness),
		RoomTimeBadness:       make(map[RoomTime]Badness),
		PinMean:               pin,
		PinStddev:             pinDev,
		ReSort:                resort,
	}

	// fill in RoomTimeBadness
	for _, room := range data.Rooms {
		for _, time := range data.Times {
			state.RoomTimeBadness[RoomTime{room, time}] = Badness{0, ""}
		}
	}

	// fill in InstructorTimeBadness
	for _, instructor := range data.Instructors {
		// start with impossible then correct it for available slots
		for _, time := range data.Times {
			state.InstructorTimeBadness[InstructorTime{instructor, time}] = impossible
		}
		for time, badness := range instructor.Times {
			state.InstructorTimeBadness[InstructorTime{instructor, time}] = badness
		}

		// fill in CourseTimeBadness
		// and prepare RoomTimeBadness list for the Section
		for _, course := range instructor.Courses {
			// start with impossible, correct it for available slots later (see below)
			for _, time := range data.Times {
				state.CourseTimeBadness[CourseTime{course, time}] = impossible
			}

			// record available room/time pairs for this course
			var roomTimeOptions []RoomTimeBadness
			for room, roomBadness := range course.Rooms {
				// intersect course times with instructor times
				for time, instructorTimeBadness := range instructor.Times {
					courseTimeBadness, present := course.Times[time]

					// combine the course time badness with instructor time badness
					switch {
					case len(course.Times) == 0:
						// use the instructor time badness
						courseTimeBadness = instructorTimeBadness
					case !present:
						// this course has allowed times, but this is not one of them
						courseTimeBadness = impossible
					default:
						// pick between instructor and course restraints
						courseTimeBadness = worst(courseTimeBadness, instructorTimeBadness)
					}

					// if course requires multiple time slots, make sure this time has
					// following slots
					for t, remaining := time.Next, course.Slots-1; remaining > 0; remaining-- {
						if t == nil {
							courseTimeBadness = impossible
							break
						}
						t = t.Next
					}

					state.CourseTimeBadness[CourseTime{course, time}] = courseTimeBadness

					// make an entry for the section
					badness := worst(roomBadness, courseTimeBadness, instructorTimeBadness)
					if badness.N < 0 {
						continue
					}
					rtb := RoomTimeBadness{
						Room:    room,
						Time:    time,
						Badness: badness,
					}
					roomTimeOptions = append(roomTimeOptions, rtb)
				}
			}

			if len(roomTimeOptions) == 0 {
				log.Printf("after intersecting available times for instructor %q", instructor.Name)
				log.Printf("and course %q, no valid times are left", course.Name)
				log.Printf("this schedule is doomed to fail")
			}

			// create the section
			section := &Section{
				Instructor:      instructor,
				Course:          course,
				RoomTimeOptions: roomTimeOptions,
			}
			state.Sections = append(state.Sections, section)
		}
	}

	return state
}

func (state *SearchState) Clone() *SearchState {
	new := &SearchState{
		Data: state.Data,
		InstructorTimeBadness: make(map[InstructorTime]Badness),
		CourseTimeBadness:     make(map[CourseTime]Badness),
		RoomTimeBadness:       make(map[RoomTime]Badness),
		PinMean:               state.PinMean,
		PinStddev:             state.PinStddev,
		ReSort:                state.ReSort,
		Generation:            state.Generation,
	}

	for _, elt := range state.Sections {
		new.Sections = append(new.Sections, elt)
	}
	for k, v := range state.InstructorTimeBadness {
		new.InstructorTimeBadness[k] = v
	}
	for k, v := range state.CourseTimeBadness {
		new.CourseTimeBadness[k] = v
	}
	for k, v := range state.RoomTimeBadness {
		new.RoomTimeBadness[k] = v
	}
	return new
}

func worst(lst ...Badness) Badness {
	bad := impossible
	for i, n := range lst {
		if n.N < 0 || n.N >= 100 {
			return impossible
		}
		if i == 0 || n.N > bad.N {
			bad = n
		}
	}
	return bad
}

func (state *SearchState) CollectRoomTimeOptions(section *Section) []RoomTimeBadness {
	var lst []RoomTimeBadness

options:
	for _, rtb := range section.RoomTimeOptions {
		if badness := state.RoomTimeBadness[RoomTime{rtb.Room, rtb.Time}]; badness.N < 0 {
			continue
		}
		for t, remaining := rtb.Time.Next, section.Course.Slots-1; remaining > 0; remaining-- {
			if badness := state.RoomTimeBadness[RoomTime{rtb.Room, t}]; badness.N < 0 {
				continue options
			}
			t = t.Next
		}

		instructorBadness := state.InstructorTimeBadness[InstructorTime{section.Instructor, rtb.Time}]
		courseBadness := state.CourseTimeBadness[CourseTime{section.Course, rtb.Time}]
		if badness := worst(rtb.Badness, instructorBadness, courseBadness); badness.N >= 0 {
			lst = append(lst, RoomTimeBadness{
				Room:    rtb.Room,
				Time:    rtb.Time,
				Badness: badness,
			})
		}
	}
	return lst
}

func (state *SearchState) SortSections(sections []*Section) {
	options := make(map[*Section]int)
	for _, section := range sections {
		tickets := 0
		for _, rtb := range state.CollectRoomTimeOptions(section) {
			if rtb.Badness.N >= 0 {
				tickets += 100 - rtb.Badness.N
			}
		}
		options[section] = tickets
	}
	sort.Slice(sections, func(a, b int) bool {
		return options[sections[a]] < options[sections[b]]
	})
}

func main() {
	rand.Seed(time.Now().UnixNano())
	log.SetFlags(log.Ltime)

	workers := runtime.NumCPU()
	dur := time.Minute
	pin := 93.0
	pinDev := 5.0
	reSort := 15
	reStart := 30 * time.Second
	inFile := "input.txt"
	outPrefix := "schedule"

	flag.IntVar(&workers, "workers", workers, "number of concurrent workers")
	flag.Float64Var(&pin, "pin", pin, "percent chance mean that a pin will be honored")
	flag.Float64Var(&pinDev, "pindev", pinDev, "percent chance stddev that a pin will be honored")
	flag.IntVar(&reSort, "sort", reSort, "how often to re-sort sections to be placed")
	flag.DurationVar(&dur, "time", dur, "total time to spend searching")
	flag.DurationVar(&reStart, "restart", reStart, "start again after this long with no improvements")
	flag.StringVar(&inFile, "in", inFile, "input file name")
	flag.StringVar(&outPrefix, "out", outPrefix, "output file prefix (.txt and .html suffixes)")
	flag.Parse()
	if flag.NArg() != 0 {
		flag.PrintDefaults()
		log.Fatalf("Usage: %s [options]", os.Args[0])
	}
	if workers < 1 {
		log.Fatalf("workers must be >= 1")
	}
	if pin < 0.0 || pin > 100.0 {
		log.Fatalf("pin must be between 0 and 100")
	}
	if pinDev < 0.0 {
		log.Fatalf("pindev must be >= 0")
	}
	if dur <= 0 {
		log.Fatalf("time must be > 0")
	}

	data := &DataSet{
		Instructors: make(map[string]*Instructor),
		Rooms:       make(map[string]*Room),
		Times:       make(map[string]*Time),

		TagToRooms: make(map[string][]*Room),
		TagToTimes: make(map[string][]*Time),
	}

	// parse everything
	if err := data.Parse(inFile); err != nil {
		log.Fatalf("%v", err)
	}
	log.Printf("finding the minimum possible number of rooms for each instructor")
	findMinRooms(data.Instructors)
	pristine := NewSearchState(data, pin, pinDev, reSort)

	generation := 0
	var mutex sync.RWMutex

	log.Printf("searching for %v with pins honored at %f%% (stddev %f%%)", dur, pin, pinDev)
	start := time.Now()

	// one goroutine gathers results
	results := make(chan *SearchState, workers)
	resultsFinished := make(chan struct{})
	go func() {
		attempts, total, count := 0, 0, 0
		bestScore := -1
		currentScore := -1
		lastReport := start
		lastBest := start
		for result := range results {
			attempts++

			// failed attempt?
			if result.Badness < 0 {
				continue
			}

			// new best for this generation?
			if result.Generation == generation && (currentScore < 0 || result.Badness <= currentScore) {
				if currentScore < 0 || result.Badness < currentScore {
					log.Printf("schedule found with badness %d", result.Badness)
					currentScore = result.Badness
					lastBest = time.Now()
				}
				mutex.Lock()
				rePin(data, result)
				mutex.Unlock()
			}

			// is this an all-time best?
			if bestScore < 0 || result.Badness < bestScore {
				log.Printf("new best score, saving result")
				bestScore = result.Badness

				// save the HTML format
				fp, err := os.Create(outPrefix + ".html")
				if err != nil {
					log.Fatalf("%v", err)
				}
				writeRoomByTime(fp, result)
				fp.Close()

				// save the CSV format
				fp, err = os.Create(outPrefix + ".txt")
				if err != nil {
					log.Fatalf("%v", err)
				}
				save(false, fp, data, result)
				fp.Close()
			}

			if time.Since(lastReport) > time.Minute {
				log.Printf("so far: %d runs in %v, badness score of %d",
					attempts, round(time.Since(start), time.Second), bestScore)
				lastReport = time.Now()
			}

			if time.Since(lastBest) > reStart {
				log.Printf("no improvements for %v, restarting", round(time.Since(lastBest), time.Second))
				lastBest = time.Now()
				currentScore = -1

				mutex.Lock()
				generation++
				unPin(data)
				mutex.Unlock()
			}

			total += result.Badness
			count++
		}

		if count > 0 {
			log.Printf("best schedule found has badness %d", bestScore)
		}
		log.Printf("%d successful runs out of %d attempts with %d generations in %v",
			count, attempts, generation+1, round(time.Since(start), time.Second))
		resultsFinished <- struct{}{}
	}()

	{
		// pin at 100% to establish a baseline
		mutex.Lock()
		state := pristine.Clone()
		state.Generation = generation
		state.PinMean = 100.0
		state.PinStddev = 0.0
		state.Solve(0)
		state.Complain()
		mutex.Unlock()
		results <- state
	}

	// other goroutines run jobs
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for time.Since(start) < dur {
				mutex.RLock()
				state := pristine.Clone()
				state.Generation = generation
				state.Solve(0)
				state.Complain()
				mutex.RUnlock()
				results <- state
			}
		}()
	}

	wg.Wait()
	close(results)
	<-resultsFinished
}

func (state *SearchState) Solve(head int) {
	if head == len(state.Sections) {
		// success
		return
	}

	// sort remaining sections by number of options
	if head%state.ReSort == 0 {
		state.SortSections(state.Sections[head:])
	}

	// pick an assignment for the first section in the list
	section := state.Sections[head]

	// start with the list of all available options
	options := state.CollectRoomTimeOptions(section)

	// consider pinned courses:
	// if PinMean is 100%, only the pinned value is acceptable
	// if < 100%, then check if it is even an option anymore
	// and if so, pick it with the PinMean chance before falling
	// back to a normal lottery
	if section.Course.PinRoom != nil || section.Course.PinTime != nil {
		if usePin(state.PinMean, state.PinStddev) {
			var altOptions []RoomTimeBadness
			for _, elt := range options {
				if (section.Course.PinRoom == nil || elt.Room == section.Course.PinRoom) &&
					(section.Course.PinTime == nil || elt.Time == section.Course.PinTime) {
					altOptions = append(altOptions, elt)
				}
			}
			if len(altOptions) > 0 {
				options = altOptions
			}
		}
	}

	if len(options) == 0 {
		// failure
		state.Badness = -1
		return
	}

	// run a lottery to pick the next choice
	tickets := 0
	for _, elt := range options {
		tickets += 100 - elt.Badness.N
	}
	winner := rand.Intn(tickets)
	var rtb RoomTimeBadness
	for _, elt := range options {
		winner -= 100 - elt.Badness.N
		if winner < 0 {
			rtb = elt
			break
		}
	}

	// place the next section
	state.RoomTimeBadness[RoomTime{rtb.Room, rtb.Time}] = impossible
	state.InstructorTimeBadness[InstructorTime{section.Instructor, rtb.Time}] = impossible
	for t, remaining := rtb.Time.Next, section.Course.Slots-1; remaining > 0; remaining-- {
		state.RoomTimeBadness[RoomTime{rtb.Room, t}] = impossible
		state.InstructorTimeBadness[InstructorTime{section.Instructor, t}] = impossible
		t = t.Next
	}
	for other, badness := range section.Course.Conflicts {
		old := state.CourseTimeBadness[CourseTime{other, rtb.Time}]
		state.CourseTimeBadness[CourseTime{other, rtb.Time}] = worst(old, badness)
		for t, remaining := rtb.Time.Next, section.Course.Slots-1; remaining > 0; remaining-- {
			old := state.CourseTimeBadness[CourseTime{other, t}]
			state.CourseTimeBadness[CourseTime{other, t}] = worst(old, badness)
			t = t.Next
		}
	}

	// report the pick
	assignment := &CoursePlacement{
		Course:  section.Course,
		Room:    rtb.Room,
		Time:    rtb.Time,
		Badness: rtb.Badness,
	}
	state.Badness += rtb.Badness.N
	state.Schedule = append(state.Schedule, assignment)
	state.Solve(head + 1)
}

func rePin(data *DataSet, state *SearchState) {
	courseToPlacement := make(map[*Course]*CoursePlacement)
	for _, elt := range state.Schedule {
		courseToPlacement[elt.Course] = elt
	}

	for _, instructor := range data.Instructors {
		for _, course := range instructor.Courses {
			course.PinRoom = courseToPlacement[course].Room
			course.PinTime = courseToPlacement[course].Time
		}
	}
}

func unPin(data *DataSet) {
	for _, instructor := range data.Instructors {
		for _, course := range instructor.Courses {
			course.PinRoom = nil
			course.PinTime = nil
		}
	}
}

func round(d time.Duration, nearest time.Duration) time.Duration {
	if nearest <= 1 {
		return d
	}
	r := d % nearest
	if r+r >= nearest {
		return d - r + nearest
	}
	return d - r
}

func (state *SearchState) Complain() {
	if state.Badness < 0 {
		return
	}

	// find what count as days (multiple time slots with the same prefix)
	timesPerDay := make(map[string]int)
	for _, time := range state.Data.Times {
		if prefix := time.Prefix(); prefix != "" {
			timesPerDay[prefix]++
		}
	}

	instructorToPlacements := make(map[*Instructor][]*CoursePlacement)
	for _, elt := range state.Schedule {
		lst := instructorToPlacements[elt.Course.Instructor]
		instructorToPlacements[elt.Course.Instructor] = append(lst, elt)
	}

	for instructor, placements := range instructorToPlacements {
		// penalize instructors with spread out schedules on a given day
		sort.Slice(placements, func(a, b int) bool {
			return placements[a].Time.Position < placements[b].Time.Position
		})

		bad := 0
		for i, a := range placements[:len(placements)-1] {
			b := placements[i+1]
			aPrefix := a.Time.Prefix()
			bPrefix := b.Time.Prefix()
			if aPrefix == "" || bPrefix == "" || aPrefix != bPrefix {
				continue
			}

			gap := b.Time.Position - a.Time.Position
			if gap < 2 {
				continue
			}
			bad += gap * gap
		}

		// special case: when packing everything on one day, try to spread it out a little
		if instructor.Days == 1 {
			bad = bad - 4
			if bad < 0 {
				bad = -bad
			}
		}
		if bad > 0 {
			state.Badness += bad
			note := fmt.Sprintf("Added %2d because %s has gaps between classes", bad, instructor.Name)
			state.BadNotes = append(state.BadNotes, note)
		}

		// penalize instructors with courses in too many rooms
		inRoom := make(map[*Room]struct{})
		for _, elt := range placements {
			inRoom[elt.Room] = struct{}{}
		}
		if extra := len(inRoom) - instructor.MinRooms; extra > 0 {
			bad := extra * extra
			state.Badness += bad
			note := fmt.Sprintf("Added %2d because %s is scheduled across more rooms than the minimum", bad, instructor.Name)
			state.BadNotes = append(state.BadNotes, note)
		}

		// how many courses does the instructor have on each day?
		onDay := make(map[string]int)
		for _, elt := range placements {
			// find how many classes this instructor has on each day
			// only consider days with multiple slots (no evenings, online, etc.)
			if prefix := elt.Time.Prefix(); timesPerDay[prefix] > 1 {
				onDay[prefix]++
			}
		}

		// try to honor instructor preferences for number of days teaching
		if instructor.Days > 0 && len(onDay) != instructor.Days {
			gap := instructor.Days - len(onDay)
			if gap < 0 {
				gap = -gap
			}
			state.Badness += 10 * gap
			s := "s"
			if len(onDay) == 1 {
				s = ""
			}
			note := fmt.Sprintf("Added %2d because courses for %s were placed on %d day%s",
				10*gap, instructor.Name, len(onDay), s)
			state.BadNotes = append(state.BadNotes, note)
		}

		// penalize workloads that are unevenly split across days
		if len(onDay) > 1 {
			// only interesting if instructor has classes on at least two days
			max, min := -1, -1
			i := 0
			for _, count := range onDay {
				if i == 0 || count > max {
					max = count
				}
				if i == 0 || count < min {
					min = count
				}
				i++
			}

			// add a penalty if there is more than one class difference
			// between the most and fewest on a day
			if gap := max - min; gap > 1 {
				state.Badness += gap * gap
				note := fmt.Sprintf("Added %2d because %s has more classes on some days than others",
					gap*gap, instructor.Name)
				state.BadNotes = append(state.BadNotes, note)
			}
		}
	}
}

func usePin(pin, stddev float64) bool {
	if pin >= 100.0 {
		return true
	}
	if pin <= 0.0 {
		return false
	}
	for {
		n := rand.NormFloat64()*stddev + pin
		if n >= 100.0 || n < 0.0 {
			continue
		}
		return rand.Float64()*100.0 < n
	}
}

// find the minimum set of rooms necessary for an instructor
// to cover all assigned courses.
// note: this is the hitting set problem, which is np-complete.
// our n is the number of courses a single instructor teaches, so
// we just brute force it
func findMinRooms(instructors map[string]*Instructor) {
	for _, instructor := range instructors {
		// get a complete list of rooms the instructor can use
		allRooms := make(map[*Room]struct{})
		for _, course := range instructor.Courses {
			for room := range course.Rooms {
				allRooms[room] = struct{}{}
			}
		}
		rooms := make([]*Room, len(allRooms))
		for room := range allRooms {
			rooms = append(rooms, room)
		}

		// note: if the loop ends without finding a solution with
		// fewer than the max number of rooms, it will leave the
		// result at the max number without bothering to prove it
	minRoomsLoop:
		for instructor.MinRooms = 1; instructor.MinRooms < len(instructor.Courses); instructor.MinRooms++ {
			n, k := len(rooms), instructor.MinRooms
			set := nChooseKInit(n, k)

		setLoop:
			for nChooseKNext(set, n, k) {
			courseLoop:
				for _, course := range instructor.Courses {
					for _, roomN := range set {
						if _, found := course.Rooms[rooms[roomN]]; found {
							continue courseLoop
						}
					}
					continue setLoop
				}

				// success!
				break minRoomsLoop
			}
		}
	}
}

func nChooseKInit(n, k int) []int {
	if k > n || n < 1 {
		panic("n choose k got bad inputs")
	}
	lst := make([]int, k)
	for i := range lst {
		lst[i] = -1
	}
	return lst
}

func nChooseKNext(lst []int, n, k int) bool {
	if lst[0] == -1 {
		for i := 0; i < k; i++ {
			lst[i] = i
		}
		return true
	}
	for i := 0; i < k; i++ {
		elt := lst[k-1-i]
		if elt < n-1-i {
			for j := k - 1 - i; j < k; j++ {
				elt++
				lst[j] = elt
			}
			return true
		}
	}
	return false
}
