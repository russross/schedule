Schedule optimizer
==================

This is a tool to generate course schedules, also known as the
timetabling problem. This is mainly intended for my own use.


About
-----

This runs as a command-line tool when generating and optimizing
schedules. It can also run with a web client to present a completed
schedule and to allow a human operator to tweak the schedule
manually.

Schedules are scored with a *badness* score, where lower is better.
When certain rooms, times, or conflicts are given a badness score in
the input data, it means that using that room/time or creating that
conflict adds the corresponding score to the overall rating for the
schedule. Badness scores for soft constraints range from 0 (ideal)
to 99 (worst score that is still permitted), with 100 or -1
indicating something is impossible (a hard constraint).



Installation
------------

You must have Go 1.11 or higher installed. Clone the package and run
`go install` to compile and install the main command-line tool.

To present results on a web page, you must have a functioning web
server where you can serve a few static files. See the section below
for instructions on compiling it.


`schedule.txt`
--------------

The input parameters are written in a file normally called
`schedule.txt`. It has the following sections:

*   Rooms: list of rooms available
*   Times: the time slots available for scheduling
*   Instructors and courses: a list of instructors and the courses
    each will teach, along with constraints on appropriate times and
    rooms.
*   Conflicts: information about curriculum conflicts, i.e., courses
    that must not be taught at overlapping times.

Blank lines and comments (starting with `//` and extending to the
end of the line) are ignored.


### Rooms

An example of room specifications might look like this:

    room: 107 computers pcs
    room: 108 computers pcs
    room: 109 computers macs printer
    room: 112 computers macs
    room: 113 computers macs printer
    room: 116 nocomputers
    room: 117 nocomputers

This says that there are 7 rooms available, named "107", "108", etc.
The names are arbitrary but must not contain spaces. The rooms are
also tagged. When referring to these rooms later, "computers" will
refer to rooms 107, 108, 109, 112, and 113, while "pcs" will only
refer to 107 and 108. This allows course room requirements to be
specified conveniently.


### Times

The times specification might look like:

    time: MWF0800 mwf
    time: MWF0900 mwf morning
    time: MWF1000 mwf morning
    time: MWF1100 mwf morning
    time: MWF1200 mwf noon
    time: MWF1300 mwf afternoon
    time: MWF1400 mwf afternoon
    time: MWF1500 mwf afternoon
    time: MWF1600 mwf
    time:
    time: TR0900 tr morning
    time: TR1030 tr morning
    time:
    time: TR1200 tr noon
    time:
    time: TR1300 tr afternoon
    time: TR1430 tr afternoon
    time: TR1600 tr
    time:
    time: T1715 evening
    time:
    time: W1715 evening
    time:
    time: R1715 evening

This specifies "MWF0800", "MWF0900", etc., as valid time slots. A
few notes:

*   The blank `time:` entry between "MWF1600" and "TR0900" indicates
    that those times are not adjacent to each other. If a class
    requires two time slots, it cannot start at MWF1600 and carry
    over to TR0900, but it can start at MWF1500 and carry over to
    MWF1600.
*   Time slots can also be tagged for convenience. "morning" is
    shorthand for any of the five time slots with that tag.
*   A time has a *prefix* if it has any characters before the first
    digit in its name (if any). If multiple time slots share the
    same prefix, they are considered to be part of the same day. In
    the above example, all MWF-prefixed classes would be considered
    part of a day, and the TR-prefixed classes would be another day.
    The groupings only count as a day if there are multiple time
    slots in that group, so the evening times are ignored when
    considering day groupings.


### Instructors and courses

Here is an example entry for a faculty member:

    instructor: John.Smith afternoon:10 morning R1715 twodays
    course: CS1000 nocomputers
    course: CS1000 nocomputers
    course: CS4099 pcs R1715
    course: IT4950 pcs
    course: IT4950 pcs

A few notes about John's constraints:

*   The "instructor" line gives the instructor's name and time
    availability. John can teach mornings or afternoons, but
    puttings him in an afternoon time slots will incur a 10 points
    badness penalty (marked by the :10 suffix). He is also available
    Thursday evenings (R1715).
*   "twodays" means that John's schedule should be spread evenly
    across two days if possible (see above for how a day is defined).
*   A time can be specified multiple times, and the one with the
    highest badness score will count. For example, you could list
    "mwf:5" and "MWF0900:10" and all mwf times would have a badness
    score of 5, except MWF0900 would have a badness score of 10.
*   John is scheduled for 5 courses, including two sections of
    CS1000 and two sections of IT4950.
*   Room tags or room names are required on all courses, and
    indicate which rooms are permissible for a given course. A room
    designation can have a badness score attached to it, e.g.,
    "109:10", to indicate that a room assignment is less than ideal
    but still permissible.
*   A course can also be tagged with "twoslots", "threeslots", or
    "studio" to indicate that it breaks the normal bell schedule and
    occupies multiple slots. "studio" is a special designation that
    uses three slots on a MWF time and two slots on a TR time.
*   Courses can also be marked with time constraints. If they are
    omitted (as in this example) the course time constraints are
    exactly the same as the instructor's time constraints. If
    present, the intersection of course and instructor
    availabitility is used (the worse badness score is applied). In
    this example, CS4099 can only be taught on Thursday evening at
    5:15pm, which implies that John's other classes must be taught
    during the remaining available slots.


### Conflicts

Conflict entries look like:

    conflict: -1 CS2810 CS3005
    conflict: 80 CS2450 WEB3200 CS3410 CS3510 CS3600 CS4320 CS4550 CS4600
    conflict: 20 CS2450 WEB3200 CS3410 CS4600 CS3010 WEB4200 IT3150 IT4500 WEB3400

This indicates that CS2810 and CS3005 must not be scheduled at the
same time (a badness score of -1 means it is impossible; -1 and 100
are equivalent).

Courses in the 2nd group will incur an 80-point penalty for being
scheduled at overlapping times. Courses in the 3rd group will incur
a 20-point penalty if scheduled at overlapping times. If multiple
courses from a group are scheduled concurrently, the penalty will be
applied multiple times.


`schedule.json`
---------------

A generated schedule is stored in a machine-readable file called
`schedule.json`. Both `schedule.txt` (which specifies the input
parameters) and `schedule.json` (which gives a specific room and
time assignment to each course) are needed when presenting a
schedule.

If you generate a schedule and think you want to keep it, make a
copy of the `schedule.json` file. As long as you do not also change
the `schedule.txt` file, you can generate more new candidate
schedules and return to an old one by restoring the `schedule.json`
file.

Note that minor changes to `schedule.txt` may still be compatible
with a `schedule.json` file as long as the list of instructors and
course assignments has not changed. This can be useful when you have
a schedule but discover that a time or room assignment will not
work. You can update `schedule.txt` and use the "swap" or "opt"
subcommands to attempt to tweak the schedule to acommodate the
updated constraint.


Using the CLI
-------------

The command-line tool has several options, with examples given
below. In each case, run the schedule command from with the
directory where the `schedule.txt` file (and `schedule.json` file if
applicable) reside. The name can be overriden with command-line
options.

*   `schedule gen`: generate a schedule from the input parameters
    and attempt to optimize it. This command generates many
    candidate schedules, scores each one, and keeps the best one
    that it finds in `schedule.json`. If you interrupt it at any
    time, the best schedule found so far will be in `schedule.json`.
    Various parameters can tweak the search process, including
    specifying how long it should spend searching.
*   `schedule score`: show the current schedule with its score and
    its known problems. The schedule is presented in a table format,
    with time slots listed down the side and rooms across the top.
*   `schedule bycourse`: show a schedule in a list ordered by course
    name.
*   `schedule byinstructor`: show a schedule in a list ordered by
    instructor in the same order as the data was given in
    `schedule.txt`.
*   `schedule opt`: attempt to improve on a schedule without
    reseting it. This is useful if you want to devote some extra
    time to trying to squeeze out a few more points without throwing
    a schedule away and generating a fresh one from scratch.
*   `schedule swap`: attempt to improve on a schedule by performing
    every possible sequence of swaps involving a maximum number of
    courses (default 4). In other words, find the best possible
    schedule found by moving up to 4 classes around. This is
    different from the "opt" commands because "swap" does an
    exhaustive search (quick for a small number of swaps, getting
    exponentially slower with more swaps) while "opt" does a
    randomized search for a set period of time.

The main generator works using hill climbing with restarts.
Candidate schedules are generated randomly (using constraint
propogation and optional weighted placement choices) and scored. The
system then tries to improve on a schedule by generating a new
random schedule that mimics the existing placements most of the
time, but occasionally randomizes a placement. If the new schedule
is an improvement, it is used as the basis for future attempts.

The main "gen" subcommand works in phases:

1.  During a warmup period of 10 seconds or so, it generates a bunch
    of independent, random schedules. The best one found during the
    warmup period moves on.
2.  During an optimization period, it always tries to improve on the
    best schedule found so far (starting with the best from the
    warmup period and optimizing it).
3.  After a period of time with no improvements, it will restart.
    The "global" restart timeout applies when the current run (from
    warmup period to timeout) has produced the best schedule found
    so far.  The "local" restart timeout applies when the best
    schedule from warmup to timeout is still not as good as one
    found in a previous warmup/optimization cycle.

The "pin" parameter determines how likely it is that an existing
placement will be retained for a course vs. randomizing that
course's placement. A high pin percentage means it is likely that
the course will be pinned in place and not allowed to move.

The pin is randomized each time a schedule is generated, with "pin"
being the mean and "pindev" as its standard deviation. The default
parameters of 95 and 5, respectively, mean that on each run a new
pin (usually near 95) will be chosen. Each time the system goes to
place a course, it will randomly choose to retain the old placement
if possible (if the 
The "pin" parameter determines how likely it is that an existing
placement will be retained for a course vs. randomizing that
course's placement. A high pin percentage means it is likely that
the course will be pinned in place and not allowed to move.

The pin is randomized each time a schedule is generated, with "pin"
being the mean and "pindev" as its standard deviation. The default
parameters of 95 and 5, respectively, mean that on each run a new
pin (usually near 95) will be chosen. Each time the system goes to
place a course, it will randomly choose to retain the old placement
if possible (if the 
The "pin" parameter determines how likely it is that an existing
placement will be retained for a course vs. randomizing that
course's placement. A high pin percentage means it is likely that
the course will be pinned in place and not allowed to move.

The pin is randomized each time a schedule is generated, with "pin"
being the mean and "pindev" as its standard deviation. The default
parameters of 95 and 5, respectively, mean that on each run a new
pin (usually near 95) will be chosen. Each time the system goes to
place a course, it will randomly choose to retain the old placement
if possible (if the 
The "pin" parameter determines how likely it is that an existing
placement will be retained for a course vs. randomizing that
course's placement. A high pin percentage means it is likely that
the course will be pinned in place and not allowed to move.

The pin is randomized each time a schedule is generated, with "pin"
being the mean and "pindev" as its standard deviation. The default
parameters of 95 and 5, respectively, mean that on each run a new
pin (usually near 95) will be chosen. Say this value is 93. Each
time the system goes to place a course, it will roll the dice and
choose to retain the old placement if possible (if the value rolled
is less than 93) or it will discard the old placement and randomly
choose a valid slot otherwise.

Generally high pin values allow it to "tweak" a schedule layout,
which low pin values will tend to shuffle it more dramatically.

The same process applies for the "opt" subcommand, except that it
always starts with the current `schedule.json` (no warmup period)
and never performs a restart.


Web page
--------

To present the results on a web page, you must compile the code to
web assembly using the experimental support in Go 1.11.

Compile using:

    GOOS=js GOARCH=wasm go build -o schedule.wasm

Copy the following to a web server:

*   `index.html`
*   `schedule.json`
*   `schedule.txt`
*   `schedule.wasm`
*   `wasm_exec.js` (get this from `$(go env GOROOT)/misc/wasm/wasm_exec.js`)

Note that your web server must server `schedule.wasm` with MIME type
`application/wasm`. For apache, you may need to create a `.htaccess`
file in the directory with this line:

    AddType application/wasm .wasm

Point your browser at this (tested in Chrome) and it will render the
schedule, its score, and the list of known problems. It will also
allow you to drag and drop classes around, instantly recalculating
the score and known problems each time. When you do this, a link
will appear at the bottom to let you download the revised schedule
as a `.json` file (to replace the `schedule.json` file).
