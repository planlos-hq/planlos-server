select
    id,
    name,
    hex,
    ($2::text is null
        or name = $2)
    and ($3::text is null
        or upper(hex) = upper($3)) as matches
from
    course_color
where
    id = $1
for update
