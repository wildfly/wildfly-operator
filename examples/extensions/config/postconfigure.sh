#!/bin/bash

echo "========== Executing postconfigure.sh =========="

for ((i=0;i<5;i++)); do echo ${i}; sleep 1; done

echo "========== End postconfigure.sh =========="