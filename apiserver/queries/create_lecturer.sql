insert into lecturer (first_name, last_name, abbreviation, kind, notes,
    isRelevant, extra_data, title)
    values ($1, $2, $3, $4, $5, $6, $7, $8)
returning
    id, first_name, last_name, abbreviation, kind, notes, isRelevant, extra_data, title
