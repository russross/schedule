package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

func (data *InputData) ReadJSON(r io.Reader) ([]Placement, error) {
	// parse the JSON
	decoder := json.NewDecoder(r)
	var sched map[string][][]string
	if err := decoder.Decode(&sched); err != nil {
		return nil, err
	}

	// build the list of placements
	var out []Placement
	for _, instructor := range data.Instructors {
		courseList := sched[instructor.Name]
		if len(courseList) != len(instructor.Courses) {
			return nil, fmt.Errorf("found %d courses for %s, but expected to find %d",
				len(courseList), instructor.Name, len(instructor.Courses))
		}
		for i := 0; i < len(courseList); i++ {
			// for co-teaching, use the first listed instructor as the canonical entry
			if instructor.Courses[i].Instructors[0] != instructor {
				continue
			}
			course := courseList[i]
			if len(course) != 3 {
				return nil, fmt.Errorf("malformed entry for course #%d of instructor %s", i+1, instructor.Name)
			}
			if course[0] != instructor.Courses[i].Name {
				return nil, fmt.Errorf("instructor %s course #%d should be %s but I found %s instead",
					instructor.Name, i+1, instructor.Courses[i].Name, course[0])
			}

			var r int
			for r = 0; r < len(data.Rooms) && course[1] != data.Rooms[r].Name; r++ {
			}
			if r >= len(data.Rooms) {
				return nil, fmt.Errorf("instructor %s course %s (#%d) has unrecognized room name %q",
					instructor.Name, instructor.Courses[i].Name, i+1, course[1])
			}

			var t int
			for t = 0; t < len(data.Times) && course[2] != data.Times[t].Name; t++ {
			}
			if t >= len(data.Times) {
				return nil, fmt.Errorf("instructor %s course %s (#%d) has unrecognized time name %q",
					instructor.Name, instructor.Courses[i].Name, i+1, course[2])
			}

			out = append(out, Placement{Course: instructor.Courses[i], Room: r, Time: t})
		}
	}

	if len(data.Instructors) != len(sched) {
		return nil, fmt.Errorf("expected to find schedule for %d instructors, but found %d instead",
			len(data.Instructors), len(sched))
	}

	return out, nil
}

func (data *InputData) WriteJSON(w io.Writer, placements []Placement) error {
	maxCourse, maxRoom, maxTime := 0, 0, 0
	p := make(map[*Course]Placement)

	for _, placement := range placements {
		p[placement.Course] = placement
		if len(placement.Course.Name) > maxCourse {
			maxCourse = len(placement.Course.Name)
		}
		if len(data.Rooms[placement.Room].Name) > maxRoom {
			maxRoom = len(data.Rooms[placement.Room].Name)
		}
		if len(data.Times[placement.Time].Name) > maxTime {
			maxTime = len(data.Times[placement.Time].Name)
		}
	}

	buf := new(bytes.Buffer)
	fmt.Fprintf(buf, "{\n")
	for n, instructor := range data.Instructors {
		fmt.Fprintf(buf, "    %q: [\n", instructor.Name)
		for cn, course := range instructor.Courses {
			place := p[course]
			if cn < len(instructor.Courses)-1 {
				fmt.Fprintf(buf, "        [%-*q, %-*q, %-*q],\n",
					maxCourse+2, course.Name,
					maxRoom+2, data.Rooms[place.Room].Name,
					maxTime+2, data.Times[place.Time].Name)
			} else {
				fmt.Fprintf(buf, "        [%-*q, %-*q, %-*q]\n",
					maxCourse+2, course.Name,
					maxRoom+2, data.Rooms[place.Room].Name,
					maxTime+2, data.Times[place.Time].Name)
			}
		}
		if n < len(data.Instructors)-1 {
			fmt.Fprintf(buf, "    ],\n")
		} else {
			fmt.Fprintf(buf, "    ]\n")
		}
	}
	fmt.Fprintf(buf, "}\n")

	_, err := buf.WriteTo(w)
	return err
}
