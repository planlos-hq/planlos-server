delete from ignored_resource_conflict
where slot_a = $1
    and slot_b = $2
    and resource_type = $3
    and resource_id = $4
