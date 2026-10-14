update
    course_time_slots
set
    course = coalesce($1, course),
    weekday = coalesce($2, weekday),
    at = coalesce($3::timetz, at),
    duration = coalesce($4::interval, duration),
    one_off_date = coalesce($5::date, one_off_date)
where
    id = $6
returning
    id,
    course,
    weekday,
    at::text,
    duration::text,
    one_off_date::text
