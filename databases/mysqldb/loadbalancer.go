package mysqldb

import "math/rand"

type DBLoadBalancer func([]*DbWrap) (*DbWrap, error)

func DefaultReadLoadBalancer(readDBList []*DbWrap) (*DbWrap, error) {
	return readDBList[rand.Int31n(int32(len(readDBList)))], nil
}

func DefaultWriteLoadBalancer(writeDBList []*DbWrap) (*DbWrap, error) {
	return writeDBList[rand.Int31n(int32(len(writeDBList)))], nil
}
