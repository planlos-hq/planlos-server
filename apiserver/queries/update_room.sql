update
    room
set
    number = coalesce($1, number),
    name = coalesce($2, name),
    seats = coalesce($3, seats),
    extra_data = coalesce($4, extra_data)
where
    id = $5
returning
    id,
    number,
    name,
    seats,
    extra_data
