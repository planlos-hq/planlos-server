insert into course (number, name, sws, credit_points, abbreviation, extra_data)
    values ($1, $2, $3, $4, $5, $6)
returning
    id, number, name, sws, credit_points, abbreviation, extra_data
