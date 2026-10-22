insert into course_color (id, name, hex) overriding system value
    values ($1, $2, $3)
returning
    id, name, hex
