package modes2

func (a AvrFrame) DecodeDF24() (DF24, error) {
	return DF24{
		ICAO: a.ICAO(),
	}, nil
}
