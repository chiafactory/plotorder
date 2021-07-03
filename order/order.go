package order

import "fmt"

type Order struct {
	ID string
}

func (p *Order) String() string {
	return fmt.Sprintf("[order id=%s]", p.ID)
}
