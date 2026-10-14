select
    id,
    name,
    hex
from
    course_color
where
    id = $1
