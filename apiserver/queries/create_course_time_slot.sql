insert into course_time_slots (course, weekday, at, duration, one_off_date)
    values ($1, $2, $3::timetz, $4::interval, $5::date)
returning
    id, course, weekday, at::text, duration::text, one_off_date::text
