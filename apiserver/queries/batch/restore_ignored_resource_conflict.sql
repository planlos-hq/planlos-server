insert into ignored_resource_conflict (slot_a, slot_b, resource_type, resource_id)
select
    least ($1::int, $2::int),
    greatest ($1::int, $2::int),
    $3,
    $4
where
    exists (
        select
            1
        from
            course_time_slots
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
    slot_a,
    slot_b,
    resource_type,
    resource_id
