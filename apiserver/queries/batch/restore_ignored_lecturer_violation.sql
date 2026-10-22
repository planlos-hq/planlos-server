insert into ignored_lecturer_constraint_violation (constraint_id, time_slot_id)
select
    $1,
    $2
where
    exists (
        select
            1
        from
            lecturer_negative_time_constraint
        where
            id = $1)
    and exists (
        select
            1
        from
            course_time_slots
        where
            id = $2)
on conflict do nothing
returning
    constraint_id,
    time_slot_id
