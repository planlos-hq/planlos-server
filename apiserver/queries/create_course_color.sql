insert into course_color (name, hex)
    values ($1, $2)
returning
    id, name, hex
