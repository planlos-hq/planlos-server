update
    lecturer_negative_time_constraint
set
    lecturer = coalesce($1, lecturer),
    weekday = coalesce($2, weekday),
    at = coalesce($3::timetz, at),
    duration = coalesce($4::interval, duration),
    soft = coalesce($5, soft)
where
    id = $6
returning
    id,
    lecturer,
    weekday,
    at::text,
    duration::text,
    soft
