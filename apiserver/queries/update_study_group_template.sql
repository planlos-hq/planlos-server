update
    study_group_template
set
    name = coalesce($1, name)
where
    id = $2
returning
    id,
    name
