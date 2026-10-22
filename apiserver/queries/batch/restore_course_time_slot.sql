insert into course_time_slots (id, course, weekday, at, duration, one_off_date) overriding system value
    values ($1, $2, $3, $4::timetz, $5::interval, $6::date)
returning
    id, course, weekday, at::text, duration::text, one_off_date::text
