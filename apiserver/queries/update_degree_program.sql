update
    degree_program
set
    name = coalesce($1, name),
    abbreviation = coalesce($2, abbreviation),
    extra_data = coalesce($3, extra_data)
where
    id = $4
returning
    id,
    name,
    abbreviation,
    extra_data
