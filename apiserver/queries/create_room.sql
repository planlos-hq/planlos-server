insert into room (number, name, seats, extra_data)
    values ($1, $2, $3, $4)
returning
    id, number, name, seats, extra_data
