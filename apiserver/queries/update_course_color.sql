update
    course_color
set
    name = coalesce($1, name),
    hex = coalesce($2, hex)
where
    id = $3
returning
    id,
    name,
    hex
