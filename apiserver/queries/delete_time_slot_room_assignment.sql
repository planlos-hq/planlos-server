delete from time_slot_room_assignment
where time_slot = $1
    and room = $2
