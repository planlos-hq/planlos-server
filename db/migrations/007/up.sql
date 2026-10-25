insert into schema_version_history (version)
    values (7);

create table ignored_course_lecturer_violation (
    course int not null primary key references course (id) on delete cascade
);

-- Exclude ignored courses
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
where
    not exists (
        select
            1
        from
            ignored_course_lecturer_violation iclv
        where
            iclv.course = c.id)
group by
    c.id,
    c.number,
    c.name
having
    count(*) filter (where l.kind != 'lab_engineer') = 0
        and count(*) filter (where l.kind = 'lab_engineer') > 0;
