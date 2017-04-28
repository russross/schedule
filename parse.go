package main

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (data *DataSet) Parse(filename string) error {
	result := make(chan error, 1)
	input := make(chan []string)

	go func() {
		// recently-parsed objects for context-sensitive items
		var instructor *Instructor
		var time *Time
		linenumber := 0
		var returnErr error
		for input := range input {
			if returnErr != nil {
				continue
			}
			linenumber++

			var fields []string
			for _, elt := range input {
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
			case "instructor:":
				if instructor, err = data.ParseInstructor(fields); err != nil {
					returnErr = fmt.Errorf("%q line %d: %v", filename, linenumber, err)
				}

			case "course:":
				if _, err = data.ParseCourse(fields, instructor); err != nil {
					returnErr = fmt.Errorf("%q line %d: %v", filename, linenumber, err)
				}

			case "room:":
				if _, err = data.ParseRoom(fields); err != nil {
					returnErr = fmt.Errorf("%q line %d: %v", filename, linenumber, err)
				}

			case "time:":
				if time, err = data.ParseTime(fields, time); err != nil {
					returnErr = fmt.Errorf("%q line %d: %v", filename, linenumber, err)
				}

			case "conflict:":
				if err = data.ParseConflict(fields); err != nil {
					returnErr = fmt.Errorf("%q line %d: %v", filename, linenumber, err)
				}

			default:
				returnErr = fmt.Errorf("%q line %d: unknown line", filename, linenumber)
			}
		}
		result <- returnErr
	}()

	// open file
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
			return err
		}
		defer res.Body.Close()
		reader = res.Body
	} else {
		log.Printf("reading input file %s", filename)
		fp, err := os.Open(filename)
		if err != nil {
			return err
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
				close(input)
				if err != io.EOF {
					return err
				}
				break
			}
			input <- record
		}
	} else {
		// get a line reader
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			line := scanner.Text()
			fields := strings.Fields(line)
			input <- fields
		}
		close(input)
		if err := scanner.Err(); err != nil {
			return err
		}
	}
	return <-result
}

func (data *DataSet) ParseRoom(fields []string) (*Room, error) {
	if len(fields) < 2 {
		log.Printf("expected %q", "room: name tag tag tag ...")
		return nil, fmt.Errorf("parsing error")
	}
	room := &Room{
		Name:     fields[1],
		Position: len(data.Rooms),
	}
	if data.Rooms[room.Name] != nil {
		return nil, fmt.Errorf("found duplicate room")
	}
	if data.Times[room.Name] != nil {
		return nil, fmt.Errorf("found room with name matching time name")
	}
	data.Rooms[room.Name] = room
	for _, tag := range fields[2:] {
		room.Tags = append(room.Tags, tag)
		data.TagToRooms[tag] = append(data.TagToRooms[tag], room)
	}

	return room, nil
}

func (data *DataSet) ParseTime(fields []string, prev *Time) (*Time, error) {
	if len(fields) == 1 {
		return nil, nil
	}
	time := &Time{
		Name:     fields[1],
		Position: len(data.Times),
	}
	if data.Times[time.Name] != nil {
		return nil, fmt.Errorf("found duplicate time")
	}
	if data.Rooms[time.Name] != nil {
		return nil, fmt.Errorf("found time with name matching room name")
	}
	data.Times[time.Name] = time
	if prev != nil {
		prev.Next = time
	}
	for _, tag := range fields[2:] {
		time.Tags = append(time.Tags, tag)
		data.TagToTimes[tag] = append(data.TagToTimes[tag], time)
	}

	return time, nil
}

func (data *DataSet) ParseInstructor(fields []string) (*Instructor, error) {
	if len(fields) < 3 {
		log.Printf("expected %q", "instructor: name time time ...")
		return nil, fmt.Errorf("parsing error")
	}
	instructor := &Instructor{
		Name:  fields[1],
		Times: make(map[*Time]Badness),
	}
	data.Instructors[instructor.Name] = instructor

	// parse available times
	for _, rawTag := range fields[2:] {
		tag, n, err := parseBadness(rawTag)
		if err != nil {
			log.Printf("when parsing times for instructor %s", instructor.Name)
			log.Printf("expected time of form %q but found %q", "time:badness", tag)
			return nil, err
		}
		badness := Badness{n, "instructor preferrence"}

		hits := 0
		if time, present := data.Times[tag]; present {
			if badness2, present := instructor.Times[time]; present && badness2.N > badness.N {
				instructor.Times[time] = badness2
			} else {
				instructor.Times[time] = badness
			}
			hits++
		}
		if times, present := data.TagToTimes[tag]; present {
			for _, time := range times {
				if badness2, present := instructor.Times[time]; present && badness2.N > badness.N {
					instructor.Times[time] = badness2
				} else {
					instructor.Times[time] = badness
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

	if len(instructor.Times) == 0 {
		log.Printf("no valid times found for instructor %q", instructor.Name)
		return nil, fmt.Errorf("no valid times found for instructor")
	}

	return instructor, nil
}

func (data *DataSet) ParseCourse(fields []string, instructor *Instructor) (*Course, error) {
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
		Rooms:      make(map[*Room]Badness),
		Times:      make(map[*Time]Badness),
		Conflicts:  make(map[*Course]Badness),
	}
	instructor.Courses = append(instructor.Courses, course)

	for _, rawTag := range fields[2:] {
		// handle pins
		if strings.HasPrefix(rawTag, "pin(") && strings.HasSuffix(rawTag, ")") {
			parts := strings.Split(rawTag[len("pin("):len(rawTag)-len(")")], ",")
			if len(parts) != 2 {
				log.Printf("pin must be of the form pin(room,time): found %q", rawTag)
				return nil, fmt.Errorf("parsing error")
			}
			if room, present := data.Rooms[parts[0]]; present {
				course.PinRoom = room
			} else if parts[0] == "" {
				// okay to omit the room
			} else {
				log.Printf("pinned room %q not found", parts[0])
				return nil, fmt.Errorf("unknown room")
			}
			if time, present := data.Times[parts[1]]; present {
				course.PinTime = time
			} else if parts[1] == "" {
				// okay to omit the time
			} else {
				log.Printf("pinned time %q not found", parts[1])
				return nil, fmt.Errorf("unknown time")
			}
			continue
		}

		// handle multiple slots
		if rawTag == "twoslots" {
			course.Slots = 2
			continue
		}
		if rawTag == "threeslots" {
			course.Slots = 3
			continue
		}

		// handle tags
		tag, n, err := parseBadness(rawTag)
		if err != nil {
			return nil, err
		}
		badness := Badness{n, "course preference"}

		hits := 0
		if room, present := data.Rooms[tag]; present {
			if badness2, present := course.Rooms[room]; present && badness2.N > badness.N {
				course.Rooms[room] = badness2
			} else {
				course.Rooms[room] = badness
			}
			hits++
		}
		if time, present := data.Times[tag]; present {
			if badness2, present := course.Times[time]; present && badness2.N > badness.N {
				course.Times[time] = badness2
			} else {
				course.Times[time] = badness
			}
			hits++
		}
		if rooms, present := data.TagToRooms[tag]; present {
			for _, room := range rooms {
				if badness2, present := course.Rooms[room]; present && badness2.N > badness.N {
					course.Rooms[room] = badness2
				} else {
					course.Rooms[room] = badness
				}
			}
			hits++
		}
		if times, present := data.TagToTimes[tag]; present {
			for _, time := range times {
				if badness2, present := course.Times[time]; present && badness2.N > badness.N {
					course.Times[time] = badness2
				} else {
					course.Times[time] = badness
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

	if len(course.Rooms) == 0 {
		return nil, fmt.Errorf("no rooms found for course %s", course.Name)
	}

	return course, nil
}

func (data *DataSet) ParseConflict(fields []string) error {
	if len(fields) < 4 {
		log.Printf("expected %q", "conflict: badness course1 course2 ...")
		return fmt.Errorf("parsing error")
	}

	n, err := strconv.Atoi(fields[1])
	if err != nil {
		return fmt.Errorf("error parsing badness value %q", fields[1])
	}
	if n < 0 || n >= 100 {
		n = -1
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
	s := "conflict:"
	for _, course := range courses {
		s += " " + course.Name
	}
	badness := Badness{n, s}

	for _, course := range courses {
		for _, elt := range courses {
			if course == elt {
				continue
			}

			if badness2, present := course.Conflicts[elt]; present && badness2.N > badness.N {
				course.Conflicts[elt] = badness2
			} else {
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
		if badness < 0 {
			return "", 0, fmt.Errorf("badness must be >= 0 in %q", tag)
		}
		return parts[0], badness, nil
	default:
		return "", 0, fmt.Errorf("error parsing badness value in %q", tag)
	}
}

func writeRoomByTime(out io.Writer, state *SearchState) {
	w := bufio.NewWriter(out)
	defer w.Flush()

	// collect a list of rooms and times and sort them by name
	// also, make an index to look up a course by its room and time
	var rooms []*Room
	var times []*Time
	roomKnown := make(map[*Room]bool)
	timeKnown := make(map[*Time]bool)
	byRoomTime := make(map[string]*CoursePlacement)
	for _, elt := range state.Schedule {
		if !roomKnown[elt.Room] {
			roomKnown[elt.Room] = true
			rooms = append(rooms, elt.Room)
		}
		if !timeKnown[elt.Time] {
			timeKnown[elt.Time] = true
			times = append(times, elt.Time)
		}
		byRoomTime[elt.Room.Name+":"+elt.Time.Name] = elt
	}
	sort.Slice(rooms, func(a, b int) bool {
		return rooms[a].Position < rooms[b].Position
	})
	sort.Slice(times, func(a, b int) bool {
		return times[a].Position < times[b].Position
	})

	fmt.Fprintf(w, "<!DOCTYPE html>\n")
	fmt.Fprintf(w, "<html lang=\"en\">\n")
	fmt.Fprintf(w, "<head>\n")
	fmt.Fprintf(w, "<title>Schedule of rooms by time</title>\n")
	fmt.Fprintf(w, "<style>\n")
	fmt.Fprintf(w, "  table, td { border: 1px solid darkgray; }\n")
	fmt.Fprintf(w, "  span, b { cursor: pointer; }\n")
	fmt.Fprintf(w, "</style>\n")
	fmt.Fprintf(w, "</head>\n")
	fmt.Fprintf(w, "<body>\n")
	fmt.Fprintf(w, "<table>\n")
	fmt.Fprintf(w, "<thead>\n")
	fmt.Fprintf(w, "  <tr>\n")
	fmt.Fprintf(w, "    <td>&nbsp;</td>\n")
	for _, room := range rooms {
		fmt.Fprintf(w, "    <td>%s</td>\n", html.EscapeString(room.Name))
	}
	fmt.Fprintf(w, "  </tr>\n")
	fmt.Fprintf(w, "</thead>\n")
	fmt.Fprintf(w, "<tbody>\n")
	for _, time := range times {
		fmt.Fprintf(w, "  <tr>\n")
		fmt.Fprintf(w, "    <td>%s</td>\n", html.EscapeString(time.Name))
		for _, room := range rooms {
			placement := byRoomTime[room.Name+":"+time.Name]
			if placement == nil {
				fmt.Fprintf(w, "    <td>&nbsp</td>\n")
			} else if placement.Badness.N > 0 {
				fmt.Fprintf(w, "    <td>%s<br>%s<br><b title=\"%s\">badness %d</b></td>\n",
					html.EscapeString(placement.Course.Instructor.Name),
					html.EscapeString(placement.Course.Name),
					html.EscapeString(placement.Badness.Message),
					placement.Badness.N)
			} else {
				fmt.Fprintf(w, "    <td>%s<br>%s</td>\n",
					html.EscapeString(placement.Course.Instructor.Name),
					html.EscapeString(placement.Course.Name))
			}
		}
		fmt.Fprintf(w, "  </tr>\n")
	}
	fmt.Fprintf(w, "</tbody>\n")
	fmt.Fprintf(w, "</table>\n")
	fmt.Fprintf(w, "<p>Schedule generated %s with badness %d</p>", time.Now().Format("Jan _2, 2006 at 3:04 PM"), state.Badness)
	fmt.Fprintf(w, `<script>
    (function () {
        var numbers = {};
        var next = 1;
        var tds = document.getElementsByTagName('td');
        for (var i = 0; i < tds.length; i++) {
            var parts = tds[i].innerHTML.split('<br>');
            if (parts.length > 1) {
                for (var j = 0; j < parts.length && j < 2; j++) {
                    if (!numbers.hasOwnProperty(parts[j])) {
                        numbers[parts[j]] = next;
                        next++;
                    }
                    var n = numbers[parts[j]];
                    parts[j] = '<span class="number' + n + '">' + parts[j] + '</span>';
                }
                tds[i].innerHTML = parts.join('<br>');
            }
        }
        var spans = document.getElementsByTagName('span');
        for (var i = 0; i < spans.length; i++) {
            spans[i].addEventListener('mouseenter', function (event) {
                var matches = document.getElementsByClassName(event.target.className);
                for (var j = 0; j < matches.length; j++) {
                    matches[j].parentElement.style.backgroundColor = 'palegreen';
                }
            });
            spans[i].addEventListener('mouseleave', function (event) {
                var matches = document.getElementsByClassName(event.target.className);
                for (var j = 0; j < matches.length; j++) {
                    matches[j].parentElement.style.backgroundColor = '';
                }
            });
        }
    })();
</script>
`)
	fmt.Fprintf(w, "</body>\n")
	fmt.Fprintf(w, "</html>\n")
}

func save(isCsv bool, out io.Writer, data *DataSet, state *SearchState) {
	// map courses to assigned times
	courseToPlacement := make(map[*Course]*CoursePlacement)
	if state != nil {
		for _, placement := range state.Schedule {
			courseToPlacement[placement.Course] = placement
		}
	}

	var rows [][]string

	if state != nil {
		rows = append(rows, []string{fmt.Sprintf("// score %d", state.Badness)})
	}

	// rooms
	rows = append(rows, []string{"// rooms"})
	var rooms []*Room
	for _, elt := range data.Rooms {
		rooms = append(rooms, elt)
	}
	sort.Slice(rooms, func(a, b int) bool {
		return rooms[a].Position < rooms[b].Position
	})
	for _, room := range rooms {
		row := []string{"room:", room.Name}
		row = append(row, room.Tags...)
		rows = append(rows, row)
	}

	// times
	rows = append(rows, []string{""})
	rows = append(rows, []string{"// times"})
	var times []*Time
	for _, elt := range data.Times {
		times = append(times, elt)
	}
	sort.Slice(times, func(a, b int) bool {
		return times[a].Position < times[b].Position
	})
	var prev *Time
	for i, time := range times {
		if i > 0 && (prev == nil || prev.Next != time) {
			rows = append(rows, []string{"time:"})
		}
		prev = time
		row := []string{"time:", time.Name}
		row = append(row, time.Tags...)
		rows = append(rows, row)
	}

	// instructors and courses
	rows = append(rows, []string{""})
	rows = append(rows, []string{"// instructors and courses"})
	var instructors []*Instructor
	for _, elt := range data.Instructors {
		instructors = append(instructors, elt)
	}
	sort.Slice(instructors, func(a, b int) bool {
		return instructors[a].Name < instructors[b].Name
	})
	for i, instructor := range instructors {
		if i > 0 {
			rows = append(rows, []string{""})
		}
		row := []string{"instructor:", instructor.Name}
		row = append(row, contractTimes(instructor.Times, data)...)
		rows = append(rows, row)

		// this instructor's courses
		for _, course := range instructor.Courses {
			row := []string{"course:", course.Name}

			// is the course pinned?
			if placement, present := courseToPlacement[course]; present {
				if placement.Badness.N > 0 {
					msg := fmt.Sprintf("// placing %s here added %d to the badness score: %s",
						course.Name, placement.Badness.N, placement.Badness.Message)
					rows = append(rows, []string{msg})
				}
				row = append(row, fmt.Sprintf("pin(%s,%s)", placement.Room.Name, placement.Time.Name))
			}
			row = append(row, contractRooms(course.Rooms, data)...)
			if len(course.Times) > 0 {
				row = append(row, contractTimes(course.Times, data)...)
			}
			rows = append(rows, row)
		}
	}

	// conflicts
	rows = append(rows, []string{""})
	rows = append(rows, []string{"// conflicts"})
	for _, conflict := range data.Conflicts {
		used := make(map[string]bool)
		row := []string{"conflict:", fmt.Sprintf("%d", conflict.Badness.N)}
		for _, course := range conflict.Courses {
			if used[course.Name] {
				continue
			}
			used[course.Name] = true
			row = append(row, course.Name)
		}
		rows = append(rows, row)
	}

	// write data
	buf := bufio.NewWriter(out)
	defer buf.Flush()

	if isCsv {
		w := csv.NewWriter(buf)
		defer w.Flush()

		// find longest row
		longest := 0
		for _, row := range rows {
			if len(row) > longest {
				longest = len(row)
			}
		}

		for _, row := range rows {
			for len(row) < longest {
				row = append(row, "")
			}
			w.Write(row)
		}
	} else {
		for _, row := range rows {
			s := strings.Join(row, " ")
			buf.WriteString(s)
			buf.WriteString("\n")
		}
	}
}

func contractRooms(in map[*Room]Badness, data *DataSet) []string {
	available := make(map[*Room]Badness)
	for k, v := range in {
		available[k] = v
	}
	var out []string

	var tags []string
	for tag := range data.TagToRooms {
		tags = append(tags, tag)
	}
	sort.Slice(tags, func(a, b int) bool {
		return len(data.TagToRooms[tags[a]]) > len(data.TagToRooms[tags[b]])
	})

tagloop:
	for _, tag := range tags {
		rooms := data.TagToRooms[tag]
		n := -1
		for i, room := range rooms {
			badness, present := available[room]
			if !present {
				continue tagloop
			}
			if i == 0 {
				n = badness.N
			}
			if n != badness.N {
				continue tagloop
			}
		}

		// a full matching set
		out = append(out, joinTagBadness(tag, n))

		for _, room := range rooms {
			delete(available, room)
		}
	}

	// add the remaining rooms as-is
	var otherRooms []*Room
	for room, _ := range available {
		otherRooms = append(otherRooms, room)
	}
	sort.Slice(otherRooms, func(a, b int) bool {
		return otherRooms[a].Position < otherRooms[b].Position
	})
	for _, room := range otherRooms {
		out = append(out, joinTagBadness(room.Name, available[room].N))
	}

	return out
}

func contractTimes(in map[*Time]Badness, data *DataSet) []string {
	available := make(map[*Time]Badness)
	for k, v := range in {
		available[k] = v
	}
	var out []string

tagloop:
	for tag, times := range data.TagToTimes {
		n := -1
		for i, time := range times {
			badness, present := available[time]
			if !present {
				continue tagloop
			}
			if i == 0 {
				n = badness.N
			}
			if n != badness.N {
				continue tagloop
			}
		}

		// a full matching set
		out = append(out, joinTagBadness(tag, n))

		for _, time := range times {
			delete(available, time)
		}
	}

	// add the remaining times as-is
	var otherTimes []*Time
	for time, _ := range available {
		otherTimes = append(otherTimes, time)
	}
	sort.Slice(otherTimes, func(a, b int) bool {
		return otherTimes[a].Position < otherTimes[b].Position
	})
	for _, time := range otherTimes {
		out = append(out, joinTagBadness(time.Name, available[time].N))
	}

	return out
}

func joinTagBadness(tag string, badness int) string {
	if badness == 0 {
		return tag
	}
	return fmt.Sprintf("%s:%d", tag, badness)
}
