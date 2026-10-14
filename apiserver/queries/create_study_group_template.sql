insert into study_group_template (name)
    values ($1)
returning
    id, name
