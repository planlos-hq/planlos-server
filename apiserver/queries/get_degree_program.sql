select
    id,
    name,
    abbreviation,
    extra_data
from
    degree_program
where
    id = $1
