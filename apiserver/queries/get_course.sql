select
    id,
    number,
    name,
    sws,
    credit_points,
    abbreviation,
    extra_data
from
    course
where
    id = $1
