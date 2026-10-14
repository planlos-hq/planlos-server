select
    id,
    number,
    name,
    seats,
    extra_data
from
    room
where
    id = $1
