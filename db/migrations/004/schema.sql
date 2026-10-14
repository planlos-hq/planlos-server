create table schema_version_history (
    version int primary key,
    applied_at timestamptz not null default current_timestamp
);

insert into schema_version_history (version)
    values (4);

create table lecturer (
    id int generated always as identity primary key,
    first_name text not null,
    last_name text not null,
    abbreviation text not null,
    kind text not null check (kind in ('regular', 'lab_engineer', 'adjunct_professor')),
    notes text null,
    isRelevant boolean not null default true,
    extra_data jsonb not null default '{}'::jsonb,
    title text null
);

create table lecturer_negative_time_constraint (
    id int not null generated always as identity primary key,
    lecturer int not null references lecturer (id),
    -- 0 = monday, .. 6 = sunday
    weekday smallint not null check (weekday between 0 and 6),
    at time not null,
    duration interval not null,
    soft boolean not null
);

create table room (
    id int generated always as identity primary key,
    number text not null,
    name text null,
    seats int null,
    extra_data jsonb not null default '{}'::jsonb
);

create table degree_program (
    id int generated always as identity primary key,
    name text not null unique,
    abbreviation text not null,
    extra_data jsonb not null default '{}'::jsonb
);

create type semester_season as enum (
    'winter',
    'summer'
);

create table study_group (
    id int not null generated always as identity primary key,
    semester_season semester_season not null,
    semester_year int not null,
    student_count int not null,
    degree_program int not null references degree_program (id),
    extra_data jsonb not null default '{}'::jsonb
);

create table course_color (
    id int not null generated always as identity primary key,
    name text not null,
    hex text not null check (hex ~ '^#[A-Fa-f0-9]{6}$')
);

create table course (
    id int not null generated always as identity primary key,
    number text not null,
    name text not null,
    sws decimal null,
    credit_points decimal null,
    abbreviation text not null,
    extra_data jsonb not null default '{}'::jsonb
);

create table course_color_assignment (
    course int not null references course (id),
    color int not null references course_color (id),
    primary key (course, color)
);

create table course_lecturer (
    course int not null references course (id),
    lecturer int not null references lecturer (id),
    primary key (course, lecturer)
);

create table course_study_group (
    course int not null references course (id),
    study_group int not null references study_group (id),
    primary key (course, study_group)
);

create table course_time_slots (
    id int not null generated always as identity primary key,
    course int not null references course (id),
    -- 0 = monday, .. 6 = sunday
    weekday smallint not null check (weekday between 0 and 6),
    at time not null,
    duration interval not null
);

create table time_slot_room_assignment (
    time_slot int not null references course_time_slots (id),
    room int not null references room (id),
    primary key (time_slot, room)
);

create table ignored_resource_conflict (
    slot_a int not null references course_time_slots (id) on delete cascade,
    slot_b int not null references course_time_slots (id) on delete cascade,
    resource_type text not null,
    resource_id int not null,
    primary key (slot_a, slot_b, resource_type, resource_id)
);

create table ignored_lecturer_constraint_violation (
    constraint_id int not null references lecturer_negative_time_constraint
	(id) on delete cascade,
    time_slot_id int not null references course_time_slots (id) on delete cascade,
    primary key (constraint_id, time_slot_id)
);

create table authorized_user (
    shib_user_name text not null primary key
);

-- Flat view for timeslots x ressources
create or replace view slot_resource as
-- Add rooms
select
    cts.id as time_slot_id,
    cts.course as course_id,
    cts.weekday,
    cts.at,
    cts.duration,
    'room'::text as resource_type,
    tra.room as resource_id
from
    course_time_slots cts
    join time_slot_room_assignment tra on tra.time_slot = cts.id
union all
-- Add lecturers
select
    cts.id,
    cts.course,
    cts.weekday,
    cts.at,
    cts.duration,
    'lecturer'::text,
    cl.lecturer
from
    course_time_slots cts
    join course_lecturer cl on cl.course = cts.course
union all
-- Add study groups
select
    cts.id,
    cts.course,
    cts.weekday,
    cts.at,
    cts.duration,
    'study_group'::text,
    csg.study_group
from
    course_time_slots cts
    join course_study_group csg on csg.course = cts.course;

-- View for ressource conflicts (rooms, lecturers, studygroups)
create or replace view resource_conflicts as
select
    a.resource_type,
    a.resource_id,
    a.weekday,
    a.time_slot_id as slot_a_id,
    a.course_id as slot_a_course,
    a.at as slot_a_at,
    a.duration as slot_a_duration,
    b.time_slot_id as slot_b_id,
    b.course_id as slot_b_course,
    b.at as slot_b_at,
    b.duration as slot_b_duration
from
    slot_resource a
    join slot_resource b on a.resource_type = b.resource_type -- Conflicts need to be between resources of the same type
        and a.resource_id = b.resource_id -- Ressource assigned at least twice
        and a.weekday = b.weekday -- Same weekday
        and a.time_slot_id < b.time_slot_id -- Prevent duplicates
        and a.at < b.at + b.duration
        and b.at < a.at + a.duration
        and not exists (
            select
                1
            from
                ignored_resource_conflict irc
        where
            irc.slot_a = a.time_slot_id
            and irc.slot_b = b.time_slot_id
            and irc.resource_type = a.resource_type
            and irc.resource_id = a.resource_id);

-- Overlap if a starts before b ends AND b starts before a ends
-- View for lecturer negative time constraint violations
create or replace view lecturer_constraint_violations as
select
    lntc.id as constraint_id,
    lntc.lecturer as lecturer_id,
    lntc.soft,
    lntc.weekday,
    lntc.at as constraint_at,
    lntc.duration as constraint_duration,
    cts.id as time_slot_id,
    cts.course as course_id,
    cts.at as slot_at,
    cts.duration as slot_duration
from
    lecturer_negative_time_constraint lntc
    join course_lecturer cl on cl.lecturer = lntc.lecturer
    join course_time_slots cts on cts.course = cl.course
where
    cts.weekday = lntc.weekday
    and cts.at < lntc.at + lntc.duration
    and lntc.at < cts.at + cts.duration
    and not exists (
        select
            1
        from
            ignored_lecturer_constraint_violation ilcv
        where
            ilcv.constraint_id = lntc.id
            and ilcv.time_slot_id = cts.id);

-- View for finding courses that only have a lab_engineer assigned
create or replace view course_lecturer_violations as
select
    c.id as course_id,
    c.number,
    c.name,
    count(*) filter (where l.kind = 'lab_engineer') as lab_engineer_count,
    count(*) filter (where l.kind != 'lab_engineer') as other_lecturer_count
from
    course c
    join course_lecturer cl on cl.course = c.id
    join lecturer l on l.id = cl.lecturer
group by
    c.id,
    c.number,
    c.name
having
    count(*) filter (where l.kind != 'lab_engineer') = 0
        and count(*) filter (where l.kind = 'lab_engineer') > 0;

-- Indexes for constraint checking
-- For time_slot_room_assigment - course_time_slots join
create index idx_tsra_time_slot on time_slot_room_assignment (time_slot);

-- For course_lecturer - course_time_slots join
create index idx_cl_course on course_lecturer (course);

-- For course_study_group - course_time_slots join
create index idx_csg_course on course_study_group (course);

-- For course_lecturer - lecturer_negative_time_constraint join
-- This is needed, because course_lecturer has a compound pk (course, lecturer)
create index idx_cl_lecturer on course_lecturer (lecturer);

-- For lecturer specific queries using lecturer_constraint_violations
create index idx_lntc_lecturer_weekday on lecturer_negative_time_constraint
    (lecturer, weekday);
