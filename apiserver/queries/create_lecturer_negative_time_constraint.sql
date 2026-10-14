insert into lecturer_negative_time_constraint (lecturer, weekday, at, duration, soft)
    values ($1, $2, $3::timetz, $4::interval, $5)
returning
    id, lecturer, weekday, at::text, duration::text, soft
