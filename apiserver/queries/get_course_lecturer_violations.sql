select
    course_id,
    number,
    name,
    lab_engineer_count
from
    course_lecturer_violations
order by
    number,
    course_id
