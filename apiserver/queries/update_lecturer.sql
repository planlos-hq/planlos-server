update
    lecturer
set
    first_name = coalesce($1, first_name),
    last_name = coalesce($2, last_name),
    abbreviation = coalesce($3, abbreviation),
    kind = coalesce($4, kind),
    notes = coalesce($5, notes),
    isRelevant = coalesce($6, isRelevant),
    extra_data = coalesce($7, extra_data),
    title = coalesce($8, title)
where
    id = $9
returning
    id,
    first_name,
    last_name,
    abbreviation,
    kind,
    notes,
    isRelevant,
    extra_data,
    title
