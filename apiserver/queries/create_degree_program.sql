insert into degree_program (name, abbreviation, extra_data)
    values ($1, $2, $3)
returning
    id, name, abbreviation, extra_data
