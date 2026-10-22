delete from ignored_lecturer_constraint_violation
where time_slot_id = $1
returning
    constraint_id,
    time_slot_id
