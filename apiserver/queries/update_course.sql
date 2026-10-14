update
    course
set
    number = coalesce($1, number),
    name = coalesce($2, name),
    sws = coalesce($3, sws),
    credit_points = coalesce($4, credit_points),
    abbreviation = coalesce($5, abbreviation),
    extra_data = coalesce($6, extra_data)
where
    id = $7
returning
    id,
    number,
    name,
    sws,
    credit_points,
    abbreviation,
    extra_data
