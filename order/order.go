package order

import "fmt"

type Order struct {
	ID string
}

func (p *Order) String() string {
	return fmt.Sprintf("Order [id=%s]", p.ID)
}
