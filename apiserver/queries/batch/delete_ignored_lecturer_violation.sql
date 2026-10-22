delete from ignored_lecturer_constraint_violation
where constraint_id = $1
    and time_slot_id = $2
returning
    constraint_id,
    time_slot_id
