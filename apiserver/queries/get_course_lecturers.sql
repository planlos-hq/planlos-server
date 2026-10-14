select
    l.id,
    l.first_name,
    l.last_name,
    l.abbreviation,
    l.kind,
    l.notes,
    l.isRelevant
from
    course c
    left join course_lecturer cl on c.id = cl.course
    left join lecturer l on cl.lecturer = l.id
where
    c.id = $1
order by
    l.last_name,
    l.first_name
