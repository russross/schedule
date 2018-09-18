package main

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
)

//
// data types representing input parameters
//
// badness ranges from 0 (good) to 99 (bad), or -1 for impossible
//

type InputData struct {
	Rooms       []*Room
	Times       []*Time
	Instructors []*Instructor
	Conflicts   []Conflict
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

type Instructor struct {
	Name     string
	Times    []int
	Courses  []*Course
	Days     int
	MinRooms int
}

type Course struct {
	Name       string
	Instructor *Instructor
	Rooms      []int
	Times      []int
	Slots      int
	Conflicts  map[*Course]int
}

type Conflict struct {
	Badness int
	Courses []*Course
}

func (t *Time) Prefix() string {
	brk := strings.IndexAny(t.Name, "0123456789")
	if brk < 0 {
		return ""
	}
	return t.Name[:brk]
}

// how many slots does this course
// require if it starts at this time?
func (c *Course) SlotsNeeded(t *Time) int {
	if c.Slots < 1 {
		return 1
	}
	if c.Slots != 23 {
		return c.Slots
	}

	// 23 marks studio format classes,
	// which need 3 slots on MWF and 2 on TR
	switch t.Prefix() {
	case "MWF":
		return 3
	case "TR":
		return 2
	default:
		return 23
	}
}

func Parse(filename string, lines [][]string) (*InputData, error) {
	data := new(InputData)

	// recently-parsed objects for context-sensitive items
	var instructor *Instructor
	var time *Time

	// parsing data that does not make it into the InputData struct
	rooms := make(map[string]*Room)
	times := make(map[string]*Time)
	tagToRooms := make(map[string][]*Room)
	tagToTimes := make(map[string][]*Time)

	for linenumber, line := range lines {
		var fields []string
		for _, elt := range line {
			comment := false
			if i := strings.Index(elt, "//"); i >= 0 {
				elt = elt[:i]
				comment = true
			}
			s := strings.TrimSpace(elt)
			if s != "" {
				fields = append(fields, s)
			}
			if comment {
				break
			}
		}

		// ignore blank/comment lines
		if len(fields) == 0 {
			continue
		}

		// process a line of input
		var err error
		switch fields[0] {
		case "room:":
			if _, err = data.ParseRoom(fields, rooms, times, tagToRooms, tagToTimes); err != nil {
				return nil, fmt.Errorf("%q line %d: %v", filename, linenumber+1, err)
			}

		case "time:":
			if time, err = data.ParseTime(fields, time, rooms, times, tagToRooms, tagToTimes); err != nil {
				return nil, fmt.Errorf("%q line %d: %v", filename, linenumber+1, err)
			}

		case "instructor:":
			if instructor, err = data.ParseInstructor(fields, times, tagToTimes); err != nil {
				return nil, fmt.Errorf("%q line %d: %v", filename, linenumber+1, err)
			}

		case "course:":
			if _, err = data.ParseCourse(fields, instructor, rooms, times, tagToRooms, tagToTimes); err != nil {
				return nil, fmt.Errorf("%q line %d: %v", filename, linenumber+1, err)
			}

		case "conflict:":
			if err = data.ParseConflict(fields); err != nil {
				return nil, fmt.Errorf("%q line %d: %v", filename, linenumber+1, err)
			}

		default:
			return nil, fmt.Errorf("%q line %d: unknown line", filename, linenumber+1)
		}
	}
	log.Printf("finding minimum possible number of rooms for each instructor")
	for _, instructor := range data.Instructors {
		instructor.FindMinRooms()
	}

	return data, nil
}

func (data *InputData) ParseRoom(fields []string, rooms map[string]*Room, times map[string]*Time, tagToRooms map[string][]*Room, tagToTimes map[string][]*Time) (*Room, error) {
	if len(fields) < 2 {
		log.Printf("expected %q", "room: name tag tag tag ...")
		return nil, fmt.Errorf("parsing error")
	}
	room := &Room{
		Name:     fields[1],
		Position: len(rooms),
	}
	data.Rooms = append(data.Rooms, room)

	if rooms[room.Name] != nil {
		return nil, fmt.Errorf("found duplicate room")
	}
	if times[room.Name] != nil {
		return nil, fmt.Errorf("found room with name matching time name")
	}
	if tagToTimes[room.Name] != nil {
		return nil, fmt.Errorf("found room with name matching time tag")
	}
	if tagToRooms[room.Name] != nil {
		return nil, fmt.Errorf("found room with name matching room tag")
	}
	rooms[room.Name] = room
	for _, tag := range fields[2:] {
		if rooms[tag] != nil {
			return nil, fmt.Errorf("found room tag with name matching room name")
		}
		if times[tag] != nil {
			return nil, fmt.Errorf("found room tag with name matching time name")
		}
		if tagToTimes[tag] != nil {
			return nil, fmt.Errorf("found room tag with name matching time tag")
		}
		room.Tags = append(room.Tags, tag)
		tagToRooms[tag] = append(tagToRooms[tag], room)
	}

	return room, nil
}

func (data *InputData) ParseTime(fields []string, prev *Time, rooms map[string]*Room, times map[string]*Time, tagToRooms map[string][]*Room, tagToTimes map[string][]*Time) (*Time, error) {
	if len(fields) == 1 {
		return nil, nil
	}
	time := &Time{
		Name:     fields[1],
		Position: len(times),
	}
	data.Times = append(data.Times, time)

	if times[time.Name] != nil {
		return nil, fmt.Errorf("found duplicate time")
	}
	if rooms[time.Name] != nil {
		return nil, fmt.Errorf("found time with name matching room name")
	}
	if tagToTimes[time.Name] != nil {
		return nil, fmt.Errorf("found time with name matching time tag")
	}
	if tagToRooms[time.Name] != nil {
		return nil, fmt.Errorf("found time with name matching room tag")
	}
	times[time.Name] = time
	if prev != nil {
		prev.Next = time
	}
	for _, tag := range fields[2:] {
		if rooms[tag] != nil {
			return nil, fmt.Errorf("found time tag with name matching room name")
		}
		if times[tag] != nil {
			return nil, fmt.Errorf("found time tag with name matching time name")
		}
		if tagToRooms[tag] != nil {
			return nil, fmt.Errorf("found time tag with name matching room tag")
		}
		time.Tags = append(time.Tags, tag)
		tagToTimes[tag] = append(tagToTimes[tag], time)
	}

	return time, nil
}

func (data *InputData) ParseInstructor(fields []string, times map[string]*Time, tagToTimes map[string][]*Time) (*Instructor, error) {
	if len(fields) < 3 {
		log.Printf("expected %q", "instructor: name time time ... [oneday|twodays]")
		return nil, fmt.Errorf("parsing error")
	}
	instructor := &Instructor{
		Name:  fields[1],
		Times: make([]int, len(times)),
	}
	for i := 0; i < len(instructor.Times); i++ {
		// all time slots default to impossible
		instructor.Times[i] = -1
	}
	data.Instructors = append(data.Instructors, instructor)

	// parse available times
	for _, rawTag := range fields[2:] {
		// handle days preferences
		if rawTag == "oneday" {
			instructor.Days = 1
			continue
		}
		if rawTag == "twodays" {
			instructor.Days = 2
			continue
		}

		tag, badness, err := parseBadness(rawTag)
		if err != nil {
			log.Printf("when parsing times for instructor %s", instructor.Name)
			log.Printf("expected time of form %q but found %q", "time:badness", tag)
			return nil, err
		}

		hits := 0
		if time, present := times[tag]; present {
			if existing := instructor.Times[time.Position]; existing < 0 || badness > existing {
				instructor.Times[time.Position] = badness
			}
			hits++
		}
		if times, present := tagToTimes[tag]; present {
			for _, time := range times {
				if existing := instructor.Times[time.Position]; existing < 0 || badness > existing {
					instructor.Times[time.Position] = badness
				}
			}
			hits++
		}
		if hits == 0 {
			log.Printf("unresolved tag %q in instructor %q", tag, instructor.Name)
			return nil, fmt.Errorf("unresolved tag")
		} else if hits > 1 {
			log.Printf("tag %q in instructor %q has multiple resolutions", tag, instructor.Name)
			return nil, fmt.Errorf("tag resolution error")
		}
	}

	valid := 0
	for _, elt := range instructor.Times {
		if elt >= 0 {
			valid++
		}
	}
	if valid == 0 {
		log.Printf("no valid times found for instructor %q", instructor.Name)
		return nil, fmt.Errorf("no valid times found for instructor")
	}

	return instructor, nil
}

func (data *InputData) ParseCourse(fields []string, instructor *Instructor, rooms map[string]*Room, times map[string]*Time, tagToRooms map[string][]*Room, tagToTimes map[string][]*Time) (*Course, error) {
	if len(fields) < 2 {
		log.Printf("expected %q", "course: name tag tag tag ...")
		return nil, fmt.Errorf("parsing error")
	}
	if instructor == nil {
		return nil, fmt.Errorf("course: must come after instructor")
	}
	course := &Course{
		Name:       fields[1],
		Instructor: instructor,
		Rooms:      make([]int, len(rooms)),
		Times:      make([]int, len(times)),
		Conflicts:  make(map[*Course]int),
	}
	for i := 0; i < len(course.Rooms); i++ {
		// all rooms default to impossible
		course.Rooms[i] = -1
	}
	for i := 0; i < len(course.Times); i++ {
		// all times default to impossible
		course.Times[i] = -1
	}
	instructor.Courses = append(instructor.Courses, course)

	for _, rawTag := range fields[2:] {
		// handle multiple slots
		if rawTag == "twoslots" {
			course.Slots = 2
			continue
		}
		if rawTag == "threeslots" {
			course.Slots = 3
			continue
		}
		if rawTag == "studio" {
			// 2 for TR, 3 for MWF
			course.Slots = 23
			continue
		}

		// handle tags
		tag, badness, err := parseBadness(rawTag)
		if err != nil {
			return nil, err
		}

		hits := 0
		if room, present := rooms[tag]; present {
			if existing := course.Rooms[room.Position]; existing < 0 || badness > existing {
				course.Rooms[room.Position] = badness
			}
			hits++
		}
		if time, present := times[tag]; present {
			if existing := course.Times[time.Position]; existing < 0 || badness > existing {
				course.Times[time.Position] = badness
			}
			hits++
		}
		if rooms, present := tagToRooms[tag]; present {
			for _, room := range rooms {
				if existing := course.Rooms[room.Position]; existing < 0 || badness > existing {
					course.Rooms[room.Position] = badness
				}
			}
			hits++
		}
		if times, present := tagToTimes[tag]; present {
			for _, time := range times {
				if existing := course.Times[time.Position]; existing < 0 || badness > existing {
					course.Times[time.Position] = badness
				}
			}
			hits++
		}
		if hits == 0 {
			log.Printf("unresolved course tag %q in course %q", tag, course.Name)
			return nil, fmt.Errorf("unresolved tag")
		} else if hits > 1 {
			log.Printf("course tag %q in course %q has multiple resolutions", tag, course.Name)
			return nil, fmt.Errorf("tag resolution error")
		}
	}

	valid := 0
	for _, badness := range course.Rooms {
		if badness >= 0 {
			valid++
		}
	}
	if valid == 0 {
		return nil, fmt.Errorf("no rooms found for course %s", course.Name)
	}

	// if the course does not specify any times, then we leave its list as nil
	// in which case the instructor times are all that matter
	hasTimes := false
	for _, badness := range course.Times {
		if badness >= 0 {
			hasTimes = true
			break
		}
	}
	if !hasTimes {
		course.Times = nil
	}

	return course, nil
}

func (data *InputData) ParseConflict(fields []string) error {
	if len(fields) < 4 {
		log.Printf("expected %q", "conflict: badness course1 course2 ...")
		return fmt.Errorf("parsing error")
	}

	badness, err := strconv.Atoi(fields[1])
	if err != nil {
		return fmt.Errorf("error parsing badness value %q", fields[1])
	}
	if badness < -1 {
		return fmt.Errorf("badness of a conflict cannot be less than -1")
	}
	if badness > 100 {
		return fmt.Errorf("badness of a conflict cannot be greater than 100")
	}
	if badness == 100 {
		badness = -1
	}

	var courses []*Course
	repeat := make(map[*Course]bool)
	for _, tag := range fields[2:] {
		found := false
		for _, instructor := range data.Instructors {
			for _, course := range instructor.Courses {
				if course.Name == tag {
					if repeat[course] {
						return fmt.Errorf("course %q repeated", tag)
					}
					repeat[course] = true
					found = true
					courses = append(courses, course)
				}
			}
		}
		if !found {
			return fmt.Errorf("course %q not found in conflict: line", tag)
		}
	}

	for _, course := range courses {
		for _, elt := range courses {
			if course == elt {
				continue
			}

			if existing, present := course.Conflicts[elt]; !present || badness > existing {
				course.Conflicts[elt] = badness
			}
		}
	}

	data.Conflicts = append(data.Conflicts, Conflict{Badness: badness, Courses: courses})

	return nil
}

func parseBadness(tag string) (string, int, error) {
	parts := strings.Split(tag, ":")
	switch len(parts) {
	case 1:
		return parts[0], 0, nil
	case 2:
		badness, err := strconv.Atoi(parts[1])
		if err != nil {
			return "", 0, fmt.Errorf("error parsing badness value in %q", tag)
		}
		if badness < 0 || badness > 100 {
			return "", 0, fmt.Errorf("badness must be between 0 and 100 in %q", tag)
		}
		return parts[0], badness, nil
	default:
		return "", 0, fmt.Errorf("error parsing badness value in %q", tag)
	}
}

func fetchFile(filename string) ([][]string, error) {
	var lines [][]string

	var reader io.Reader
	isCsv := false
	if strings.HasPrefix(filename, "http:") || strings.HasPrefix(filename, "https:") {
		const docsSuffix = "/edit?usp=sharing"
		if strings.HasSuffix(filename, docsSuffix) {
			filename = filename[:len(filename)-len(docsSuffix)] + "/export?format=csv"
			isCsv = true
		}
		log.Printf("downloading input URL %s", filename)
		res, err := http.Get(filename)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		reader = res.Body
	} else {
		log.Printf("reading input file %s", filename)
		fp, err := os.Open(filename)
		if err != nil {
			return nil, err
		}
		defer fp.Close()
		reader = fp
		isCsv = strings.HasSuffix(filename, ".csv")
	}

	if isCsv {
		buf := bufio.NewReader(reader)
		reader := csv.NewReader(buf)
		for {
			record, err := reader.Read()
			if err != nil {
				if err != io.EOF {
					return nil, err
				}
				break
			}
			lines = append(lines, record)
		}
	} else {
		// get a line reader
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			line := scanner.Text()
			fields := strings.Fields(line)
			lines = append(lines, fields)
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
	}
	return lines, nil
}

// find the minimum set of rooms necessary for an instructor
// to cover all assigned courses.
// note: this is the hitting set problem, which is np-complete.
// our n is the number of courses a single instructor teaches, so
// we just brute force it
func (instructor *Instructor) FindMinRooms() {
	// get a complete list of rooms the instructor can use
	allPossibleRooms := make(map[int]struct{})
	for _, course := range instructor.Courses {
		for position, badness := range course.Rooms {
			if badness >= 0 {
				allPossibleRooms[position] = struct{}{}
			}
		}
	}
	var roomPositions []int
	for position := range allPossibleRooms {
		roomPositions = append(roomPositions, position)
	}

	// note: if the loop ends without finding a solution with
	// fewer than the max number of rooms, it will leave the
	// result at the max number without bothering to prove it
minRoomsLoop:
	for instructor.MinRooms = 1; instructor.MinRooms < len(instructor.Courses); instructor.MinRooms++ {
		n, k := len(roomPositions), instructor.MinRooms
		set := nChooseKInit(n, k)

	setLoop:
		for nChooseKNext(set, n, k) {
		courseLoop:
			for _, course := range instructor.Courses {
				for _, index := range set {
					if course.Rooms[roomPositions[index]] >= 0 {
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
